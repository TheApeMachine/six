//go:build darwin && cgo
// +build darwin,cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"

static id<MTLDevice> device = nil;
static id<MTLCommandQueue> commandQueue = nil;
static id<MTLComputePipelineState> resolveResonancePipeline = nil;
static id<MTLComputePipelineState> resolvePhaseDialPipeline = nil;
static id<MTLComputePipelineState> encodePhaseDialPipeline = nil;
static id<MTLComputePipelineState> seqToroidalMeanPhasePipeline = nil;
static id<MTLComputePipelineState> weightedCircularMeanPipeline = nil;
static id<MTLComputePipelineState> solveBVPPipeline = nil;

static dispatch_once_t initOnceToken;
static int initResult = 0;

#define GFROTATION_SIZE 4

int init_metal(const char* metallib_path) {
    if (device != nil && initResult == 0) {
        return 0;
    }

    dispatch_once(&initOnceToken, ^{
        device = MTLCreateSystemDefaultDevice();
        if (!device) {
            initResult = -1;
            return;
        }

        commandQueue = [device newCommandQueue];
        if (!commandQueue) {
            device = nil;
            initResult = -2;
            return;
        }

        NSString *path = [NSString stringWithUTF8String:metallib_path];
        NSError *error = nil;
        NSURL *url = [NSURL fileURLWithPath:path];
        id<MTLLibrary> library = [device newLibraryWithURL:url error:&error];
        if (!library) {
            NSLog(@"Failed to load metallib: %@", error);
            commandQueue = nil;
            device = nil;
            initResult = -3;
            return;
        }

        id<MTLFunction> function = [library newFunctionWithName:@"resolve_resonance"];
        if (!function) {
            NSLog(@"Failed to find resolve_resonance function in metallib");
            commandQueue = nil;
            device = nil;
            initResult = -4;
            return;
        }

        resolveResonancePipeline = [device newComputePipelineStateWithFunction:function error:&error];
        if (!resolveResonancePipeline) {
            NSLog(@"Failed to create compute pipeline state: %@", error);
            resolveResonancePipeline = nil;
            commandQueue = nil;
            device = nil;
            initResult = -5;
            return;
        }

        id<MTLFunction> fPhaseDial = [library newFunctionWithName:@"resolve_phasedial"];
        if (fPhaseDial) {
            resolvePhaseDialPipeline = [device newComputePipelineStateWithFunction:fPhaseDial error:&error];
        }
        id<MTLFunction> fEncode = [library newFunctionWithName:@"encode_phasedial"];
        if (fEncode) {
            encodePhaseDialPipeline = [device newComputePipelineStateWithFunction:fEncode error:&error];
        }
        id<MTLFunction> fSeqTor = [library newFunctionWithName:@"seq_toroidal_mean_phase"];
        if (fSeqTor) {
            seqToroidalMeanPhasePipeline = [device newComputePipelineStateWithFunction:fSeqTor error:&error];
        }
        id<MTLFunction> fWCM = [library newFunctionWithName:@"weighted_circular_mean"];
        if (fWCM) {
            weightedCircularMeanPipeline = [device newComputePipelineStateWithFunction:fWCM error:&error];
        }
        id<MTLFunction> fBVP = [library newFunctionWithName:@"solve_bvp"];
        if (fBVP) {
            solveBVPPipeline = [device newComputePipelineStateWithFunction:fBVP error:&error];
        }

        initResult = 0;
    });

    return initResult;
}

int resolve_resonance_metal(const void* graph_nodes_ptr, uint32_t num_nodes, const void* active_context_ptr, uint64_t* out_result) {
    if (!resolveResonancePipeline || out_result == NULL || graph_nodes_ptr == NULL || active_context_ptr == NULL) {
        return -1;
    }
    if (num_nodes == 0) {
        *out_result = 0;
        return 0;
    }

    @autoreleasepool {
        NSUInteger nodeBytes = GFROTATION_SIZE;
        NSUInteger totalNodesSize = (NSUInteger)num_nodes * nodeBytes;

        id<MTLBuffer> ctxBuffer = [device newBufferWithLength:nodeBytes options:MTLResourceStorageModeShared];
        if (!ctxBuffer) {
            return -2;
        }

        id<MTLBuffer> resultBuffer = [device newBufferWithLength:sizeof(uint64_t) options:MTLResourceStorageModeShared];
        if (!resultBuffer) {
            [ctxBuffer release];
            return -2;
        }

        memcpy([ctxBuffer contents], active_context_ptr, nodeBytes);
        uint64_t initialValue = 0;
        memcpy([resultBuffer contents], &initialValue, sizeof(uint64_t));

        id<MTLBuffer> nodesBuffer = [device newBufferWithBytesNoCopy:(void*)graph_nodes_ptr
                                                               length:totalNodesSize
                                                              options:MTLResourceStorageModeShared
                                                          deallocator:nil];
        if (!nodesBuffer) {
            nodesBuffer = [device newBufferWithBytes:graph_nodes_ptr
                                              length:totalNodesSize
                                             options:MTLResourceStorageModeShared];
        }
        if (!nodesBuffer) {
            [ctxBuffer release];
            [resultBuffer release];
            return -3;
        }

        id<MTLCommandBuffer> commandBuffer = [commandQueue commandBuffer];
        if (!commandBuffer) {
            [nodesBuffer release];
            [ctxBuffer release];
            [resultBuffer release];
            return -4;
        }

        id<MTLComputeCommandEncoder> computeEncoder = [commandBuffer computeCommandEncoder];
        if (!computeEncoder) {
            [nodesBuffer release];
            [ctxBuffer release];
            [resultBuffer release];
            return -4;
        }

        [computeEncoder setComputePipelineState:resolveResonancePipeline];
        [computeEncoder setBuffer:nodesBuffer offset:0 atIndex:0];
        [computeEncoder setBuffer:ctxBuffer offset:0 atIndex:1];
        [computeEncoder setBuffer:resultBuffer offset:0 atIndex:2];
        [computeEncoder setBytes:&num_nodes length:sizeof(uint32_t) atIndex:3];
        uint32_t baseOffset = 0;
        [computeEncoder setBytes:&baseOffset length:sizeof(uint32_t) atIndex:4];

        NSUInteger threadWidth = resolveResonancePipeline.threadExecutionWidth;
        if (threadWidth == 0) {
            threadWidth = 1;
        }
        NSUInteger threadsPerGroup = threadWidth;
        if (threadsPerGroup > resolveResonancePipeline.maxTotalThreadsPerThreadgroup) {
            threadsPerGroup = resolveResonancePipeline.maxTotalThreadsPerThreadgroup;
        }
        if (threadsPerGroup > num_nodes) {
            threadsPerGroup = num_nodes;
        }
        if (threadsPerGroup == 0) {
            threadsPerGroup = 1;
        }

        MTLSize gridSize = MTLSizeMake(num_nodes, 1, 1);
        MTLSize threadgroupSize = MTLSizeMake(threadsPerGroup, 1, 1);
        [computeEncoder dispatchThreads:gridSize threadsPerThreadgroup:threadgroupSize];
        [computeEncoder endEncoding];

        [commandBuffer commit];
        [commandBuffer waitUntilCompleted];

        if (commandBuffer.status != MTLCommandBufferStatusCompleted) {
            [nodesBuffer release];
            [ctxBuffer release];
            [resultBuffer release];
            return -5;
        }

        uint64_t* resultPtr = (uint64_t*)[resultBuffer contents];
        *out_result = *resultPtr;

        [nodesBuffer release];
        [ctxBuffer release];
        [resultBuffer release];
        return 0;
    }
}

static int runMetalKernel(id<MTLComputePipelineState> pipeline, id<MTLComputeCommandEncoder> enc,
                         NSUInteger threadCount, NSUInteger threadsPerGroup) {
    if (!pipeline || !enc) return -1;
    [enc setComputePipelineState:pipeline];
    NSUInteger tg = threadsPerGroup;
    if (tg > pipeline.maxTotalThreadsPerThreadgroup) tg = pipeline.maxTotalThreadsPerThreadgroup;
    if (tg > threadCount) tg = threadCount;
    if (tg == 0) tg = 1;
    MTLSize gridSize = MTLSizeMake(threadCount, 1, 1);
    MTLSize threadgroupSize = MTLSizeMake(tg, 1, 1);
    [enc dispatchThreads:gridSize threadsPerThreadgroup:threadgroupSize];
    return 0;
}

int resolve_phasedial_metal(const void* cache_nodes_ptr, uint32_t num_nodes, const void* query_dial_ptr, void* similarities_ptr) {
    if (!resolvePhaseDialPipeline || !cache_nodes_ptr || !query_dial_ptr || !similarities_ptr || num_nodes == 0) return -1;
    size_t cacheBytes = (size_t)num_nodes * 1024 * sizeof(float);
    size_t queryBytes = 1024 * sizeof(float);
    size_t simBytes = num_nodes * sizeof(float);

    float* fCache = (float*)malloc(cacheBytes);
    float* fQuery = (float*)malloc(queryBytes);
    if (!fCache || !fQuery) { free(fCache); free(fQuery); return -2; }
    const double* dCache = (const double*)cache_nodes_ptr;
    const double* dQuery = (const double*)query_dial_ptr;
    for (size_t i = 0; i < (size_t)num_nodes * 1024; i++) fCache[i] = (float)dCache[i];
    for (size_t i = 0; i < 1024; i++) fQuery[i] = (float)dQuery[i];

    id<MTLBuffer> cacheBuf = [device newBufferWithBytes:fCache length:cacheBytes options:MTLResourceStorageModeShared];
    id<MTLBuffer> queryBuf = [device newBufferWithBytes:fQuery length:queryBytes options:MTLResourceStorageModeShared];
    id<MTLBuffer> simBuf = [device newBufferWithLength:simBytes options:MTLResourceStorageModeShared];
    free(fCache);
    free(fQuery);
    if (!cacheBuf || !queryBuf || !simBuf) {
        if (cacheBuf) [cacheBuf release];
        if (queryBuf) [queryBuf release];
        if (simBuf) [simBuf release];
        return -3;
    }

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:cacheBuf offset:0 atIndex:0];
    [enc setBuffer:queryBuf offset:0 atIndex:1];
    [enc setBuffer:simBuf offset:0 atIndex:2];
    [enc setBytes:&num_nodes length:sizeof(uint32_t) atIndex:3];
    NSUInteger threads = (NSUInteger)num_nodes * 32;
    int r = runMetalKernel(resolvePhaseDialPipeline, enc, threads, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];
    [cacheBuf release];
    [queryBuf release];
    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) {
        [simBuf release];
        return -4;
    }
    float* simOut = (float*)[simBuf contents];
    double* dSim = (double*)similarities_ptr;
    for (uint32_t i = 0; i < num_nodes; i++) dSim[i] = (double)simOut[i];
    [simBuf release];
    return 0;
}

int encode_phasedial_metal(const void* structural_phases_ptr, const void* primes_ptr, uint32_t num_values, void* out_dial_ptr) {
    if (!encodePhaseDialPipeline || !structural_phases_ptr || !primes_ptr || !out_dial_ptr) return -1;
    size_t phaseBytes = (size_t)num_values * sizeof(float);
    size_t primeBytes = 512 * sizeof(float);
    float* fPhases = (float*)malloc(phaseBytes);
    if (!fPhases) return -2;
    const double* dPhases = (const double*)structural_phases_ptr;
    for (uint32_t i = 0; i < num_values; i++) fPhases[i] = (float)dPhases[i];

    id<MTLBuffer> phaseBuf = [device newBufferWithBytes:fPhases length:phaseBytes options:MTLResourceStorageModeShared];
    id<MTLBuffer> primeBuf = [device newBufferWithBytes:primes_ptr length:primeBytes options:MTLResourceStorageModeShared];
    id<MTLBuffer> outBuf = [device newBufferWithLength:1024*sizeof(float) options:MTLResourceStorageModeShared];
    free(fPhases);
    if (!phaseBuf || !primeBuf || !outBuf) {
        if (phaseBuf) [phaseBuf release];
        if (primeBuf) [primeBuf release];
        if (outBuf) [outBuf release];
        return -3;
    }

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:phaseBuf offset:0 atIndex:0];
    [enc setBuffer:primeBuf offset:0 atIndex:1];
    [enc setBuffer:outBuf offset:0 atIndex:2];
    [enc setBytes:&num_values length:sizeof(uint32_t) atIndex:3];
    int r = runMetalKernel(encodePhaseDialPipeline, enc, 512, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];
    [phaseBuf release];
    [primeBuf release];
    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) {
        [outBuf release];
        return -4;
    }
    float* fOut = (float*)[outBuf contents];
    double* dOut = (double*)out_dial_ptr;
    for (size_t i = 0; i < 1024; i++) dOut[i] = (double)fOut[i];
    [outBuf release];
    return 0;
}

int seq_toroidal_mean_phase_metal(const void* value_blocks_ptr, uint32_t num_values, double* out_theta, double* out_phi) {
    if (!seqToroidalMeanPhasePipeline || !value_blocks_ptr || !out_theta || !out_phi || num_values == 0) return -1;
    size_t blockBytes = (size_t)num_values * 8 * sizeof(uint64_t);
    size_t outBytes = (size_t)num_values * 4 * sizeof(float);

    id<MTLBuffer> blockBuf = [device newBufferWithBytes:(void*)value_blocks_ptr length:blockBytes options:MTLResourceStorageModeShared];
    id<MTLBuffer> sumBuf = [device newBufferWithLength:outBytes options:MTLResourceStorageModeShared];
    if (!blockBuf || !sumBuf) {
        if (blockBuf) [blockBuf release];
        if (sumBuf) [sumBuf release];
        return -2;
    }

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:blockBuf offset:0 atIndex:0];
    [enc setBuffer:sumBuf offset:0 atIndex:1];
    [enc setBytes:&num_values length:sizeof(uint32_t) atIndex:2];
    int r = runMetalKernel(seqToroidalMeanPhasePipeline, enc, num_values, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];
    [blockBuf release];
    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) {
        [sumBuf release];
        return -3;
    }
    float* sums = (float*)[sumBuf contents];
    float sinT = 0, cosT = 0, sinP = 0, cosP = 0;
    for (uint32_t i = 0; i < num_values; i++) {
        sinT += sums[i*4+0];
        cosT += sums[i*4+1];
        sinP += sums[i*4+2];
        cosP += sums[i*4+3];
    }
    [sumBuf release];
    *out_theta = atan2((double)sinT, (double)cosT);
    double phi = atan2((double)sinP, (double)cosP);
    if (phi < 0) phi += 2.0 * 3.14159265358979323846;
    *out_phi = phi;
    return 0;
}

int weighted_circular_mean_metal(const void* value_blocks_ptr, uint32_t num_values, double* out_phase, double* out_concentration) {
    if (!weightedCircularMeanPipeline || !value_blocks_ptr || !out_phase || !out_concentration || num_values == 0) return -1;
    size_t blockBytes = (size_t)num_values * 8 * sizeof(uint64_t);
    size_t outBytes = (size_t)num_values * 3 * sizeof(float);

    id<MTLBuffer> blockBuf = [device newBufferWithBytes:(void*)value_blocks_ptr length:blockBytes options:MTLResourceStorageModeShared];
    id<MTLBuffer> sumBuf = [device newBufferWithLength:outBytes options:MTLResourceStorageModeShared];
    if (!blockBuf || !sumBuf) {
        if (blockBuf) [blockBuf release];
        if (sumBuf) [sumBuf release];
        return -2;
    }

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:blockBuf offset:0 atIndex:0];
    [enc setBuffer:sumBuf offset:0 atIndex:1];
    [enc setBytes:&num_values length:sizeof(uint32_t) atIndex:2];
    int r = runMetalKernel(weightedCircularMeanPipeline, enc, num_values, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];
    [blockBuf release];
    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) {
        [sumBuf release];
        return -3;
    }
    float* sums = (float*)[sumBuf contents];
    float wSin = 0, wCos = 0, wSum = 0;
    for (uint32_t i = 0; i < num_values; i++) {
        wSin += sums[i*3+0];
        wCos += sums[i*3+1];
        wSum += sums[i*3+2];
    }
    [sumBuf release];
    *out_phase = atan2((double)wSin, (double)wCos);
    if (*out_phase < 0) *out_phase += 2.0 * 3.14159265358979323846;
    *out_concentration = (wSum > 0) ? (double)(sqrtf(wSin*wSin + wCos*wCos) / wSum) : 0.0;
    return 0;
}

int solve_bvp_metal(const void* start_blocks_ptr, const void* goal_blocks_ptr, uint16_t* out_scale, uint16_t* out_translate, double* out_distance) {
    if (!solveBVPPipeline || !start_blocks_ptr || !goal_blocks_ptr || !out_scale || !out_translate || !out_distance) return -1;

    id<MTLBuffer> startBuf = [device newBufferWithBytes:(void*)start_blocks_ptr length:8*sizeof(uint64_t) options:MTLResourceStorageModeShared];
    id<MTLBuffer> goalBuf = [device newBufferWithBytes:(void*)goal_blocks_ptr length:8*sizeof(uint64_t) options:MTLResourceStorageModeShared];
    id<MTLBuffer> resultBuf = [device newBufferWithLength:sizeof(uint64_t) options:MTLResourceStorageModeShared];
    if (!startBuf || !goalBuf || !resultBuf) {
        if (startBuf) [startBuf release];
        if (goalBuf) [goalBuf release];
        if (resultBuf) [resultBuf release];
        return -2;
    }
    uint64_t zero = 0;
    memcpy([resultBuf contents], &zero, sizeof(uint64_t));

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:startBuf offset:0 atIndex:0];
    [enc setBuffer:goalBuf offset:0 atIndex:1];
    [enc setBuffer:resultBuf offset:0 atIndex:2];
    int r = runMetalKernel(solveBVPPipeline, enc, 65792, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];
    [startBuf release];
    [goalBuf release];
    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) {
        [resultBuf release];
        return -3;
    }
    uint64_t packed = *(uint64_t*)[resultBuf contents];
    [resultBuf release];
    uint32_t id = (uint32_t)(packed & 0xFFFFFFFFULL);
    uint32_t match_count = (uint32_t)(packed >> 32);
    *out_scale = (uint16_t)((id / 257) + 1);
    *out_translate = (uint16_t)(id % 257);
    *out_distance = 1.0 - ((double)match_count / 257.0);
    return 0;
}

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

/* ─── Persistent buffer pools ─────────────────────────────────────────── */

static id<MTLBuffer> persistentNodesBuffer = nil;
static id<MTLBuffer> persistentCtxBuffer = nil;
static id<MTLBuffer> persistentResultBuffer = nil;
static uint32_t persistentNodesCapacity = 0;

static id<MTLBuffer> persistentCacheBuf = nil;
static id<MTLBuffer> persistentQueryBuf = nil;
static id<MTLBuffer> persistentSimBuf = nil;
static uint32_t persistentPhaseDialCapacity = 0;

static id<MTLBuffer> persistentPhaseBuf = nil;
static id<MTLBuffer> persistentPrimeBuf = nil;
static id<MTLBuffer> persistentOutDialBuf = nil;
static uint32_t persistentEncodeCapacity = 0;

static id<MTLBuffer> persistentBlockBuf = nil;
static id<MTLBuffer> persistentSeqSumBuf = nil;
static id<MTLBuffer> persistentWCMSumBuf = nil;
static uint32_t persistentBlockCapacity = 0;

static id<MTLBuffer> persistentBVPStartBuf = nil;
static id<MTLBuffer> persistentBVPGoalBuf = nil;
static id<MTLBuffer> persistentBVPResultBuf = nil;
static int persistentBVPAllocated = 0;

/* ─── Pool growth functions ───────────────────────────────────────────── */

static int ensure_resonance_buffers(uint32_t num_nodes) {
    if (persistentNodesBuffer != nil && persistentNodesCapacity >= num_nodes) {
        return 0;
    }

    uint32_t capacity = num_nodes * 2;
    if (capacity < 1024) capacity = 1024;

    NSUInteger nodeBytes = GFROTATION_SIZE;
    NSUInteger totalSize = (NSUInteger)capacity * nodeBytes;

    if (persistentNodesBuffer) [persistentNodesBuffer release];
    if (persistentCtxBuffer) [persistentCtxBuffer release];
    if (persistentResultBuffer) [persistentResultBuffer release];

    persistentNodesBuffer = [device newBufferWithLength:totalSize options:MTLResourceStorageModeShared];
    persistentCtxBuffer = [device newBufferWithLength:nodeBytes options:MTLResourceStorageModeShared];
    persistentResultBuffer = [device newBufferWithLength:sizeof(uint64_t) options:MTLResourceStorageModeShared];

    if (!persistentNodesBuffer || !persistentCtxBuffer || !persistentResultBuffer) {
        persistentNodesCapacity = 0;
        return -1;
    }

    persistentNodesCapacity = capacity;
    return 0;
}

static int ensure_phasedial_buffers(uint32_t num_nodes) {
    if (persistentCacheBuf != nil && persistentPhaseDialCapacity >= num_nodes) {
        return 0;
    }

    uint32_t capacity = num_nodes * 2;
    if (capacity < 256) capacity = 256;

    if (persistentCacheBuf) [persistentCacheBuf release];
    if (persistentQueryBuf) [persistentQueryBuf release];
    if (persistentSimBuf) [persistentSimBuf release];

    size_t cacheBytes = (size_t)capacity * 1024 * sizeof(float);
    size_t queryBytes = 1024 * sizeof(float);
    size_t simBytes = (size_t)capacity * sizeof(float);

    persistentCacheBuf = [device newBufferWithLength:cacheBytes options:MTLResourceStorageModeShared];
    persistentQueryBuf = [device newBufferWithLength:queryBytes options:MTLResourceStorageModeShared];
    persistentSimBuf = [device newBufferWithLength:simBytes options:MTLResourceStorageModeShared];

    if (!persistentCacheBuf || !persistentQueryBuf || !persistentSimBuf) {
        persistentPhaseDialCapacity = 0;
        return -1;
    }

    persistentPhaseDialCapacity = capacity;
    return 0;
}

static int ensure_encode_buffers(uint32_t num_values) {
    if (persistentPrimeBuf == nil) {
        persistentPrimeBuf = [device newBufferWithLength:512 * sizeof(float) options:MTLResourceStorageModeShared];
        persistentOutDialBuf = [device newBufferWithLength:1024 * sizeof(float) options:MTLResourceStorageModeShared];

        if (!persistentPrimeBuf || !persistentOutDialBuf) return -1;
    }

    if (persistentPhaseBuf != nil && persistentEncodeCapacity >= num_values) {
        return 0;
    }

    if (persistentPhaseBuf) [persistentPhaseBuf release];

    uint32_t capacity = num_values * 2;
    if (capacity < 256) capacity = 256;

    persistentPhaseBuf = [device newBufferWithLength:(size_t)capacity * sizeof(float) options:MTLResourceStorageModeShared];

    if (!persistentPhaseBuf) { persistentEncodeCapacity = 0; return -1; }

    persistentEncodeCapacity = capacity;
    return 0;
}

static int ensure_block_buffers(uint32_t num_values) {
    if (persistentBlockBuf != nil && persistentBlockCapacity >= num_values) {
        return 0;
    }

    uint32_t capacity = num_values * 2;
    if (capacity < 256) capacity = 256;

    if (persistentBlockBuf) [persistentBlockBuf release];
    if (persistentSeqSumBuf) [persistentSeqSumBuf release];
    if (persistentWCMSumBuf) [persistentWCMSumBuf release];

    size_t blockBytes = (size_t)capacity * 8 * sizeof(uint64_t);
    size_t seqOutBytes = (size_t)capacity * 4 * sizeof(float);
    size_t wcmOutBytes = (size_t)capacity * 3 * sizeof(float);

    persistentBlockBuf = [device newBufferWithLength:blockBytes options:MTLResourceStorageModeShared];
    persistentSeqSumBuf = [device newBufferWithLength:seqOutBytes options:MTLResourceStorageModeShared];
    persistentWCMSumBuf = [device newBufferWithLength:wcmOutBytes options:MTLResourceStorageModeShared];

    if (!persistentBlockBuf || !persistentSeqSumBuf || !persistentWCMSumBuf) {
        persistentBlockCapacity = 0;
        return -1;
    }

    persistentBlockCapacity = capacity;
    return 0;
}

static int ensure_bvp_buffers(void) {
    if (persistentBVPAllocated) return 0;

    persistentBVPStartBuf = [device newBufferWithLength:8 * sizeof(uint64_t) options:MTLResourceStorageModeShared];
    persistentBVPGoalBuf = [device newBufferWithLength:8 * sizeof(uint64_t) options:MTLResourceStorageModeShared];
    persistentBVPResultBuf = [device newBufferWithLength:sizeof(uint64_t) options:MTLResourceStorageModeShared];

    if (!persistentBVPStartBuf || !persistentBVPGoalBuf || !persistentBVPResultBuf) return -1;

    persistentBVPAllocated = 1;
    return 0;
}

/* ─── Device init ─────────────────────────────────────────────────────── */

int count_metal_devices(void) {
    NSArray<id<MTLDevice>> *devices = MTLCopyAllDevices();
    if (!devices) return 0;
    int count = (int)[devices count];
    [devices release];
    return count;
}

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

/* ─── Kernel dispatch helper ──────────────────────────────────────────── */

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

/* ─── Host dispatch functions ─────────────────────────────────────────── */

int resolve_resonance_metal(const void* graph_nodes_ptr, uint32_t num_nodes, const void* active_context_ptr, uint64_t* out_result) {
    if (!resolveResonancePipeline || out_result == NULL || graph_nodes_ptr == NULL || active_context_ptr == NULL) {
        return -1;
    }

    if (num_nodes == 0) {
        *out_result = 0;
        return 0;
    }

    if (ensure_resonance_buffers(num_nodes) != 0) return -2;

    @autoreleasepool {
        NSUInteger nodeBytes = GFROTATION_SIZE;
        NSUInteger totalNodesSize = (NSUInteger)num_nodes * nodeBytes;

        memcpy([persistentNodesBuffer contents], graph_nodes_ptr, totalNodesSize);
        memcpy([persistentCtxBuffer contents], active_context_ptr, nodeBytes);
        uint64_t initialValue = 0;
        memcpy([persistentResultBuffer contents], &initialValue, sizeof(uint64_t));

        id<MTLCommandBuffer> commandBuffer = [commandQueue commandBuffer];
        if (!commandBuffer) return -4;

        id<MTLComputeCommandEncoder> computeEncoder = [commandBuffer computeCommandEncoder];
        if (!computeEncoder) return -4;

        [computeEncoder setComputePipelineState:resolveResonancePipeline];
        [computeEncoder setBuffer:persistentNodesBuffer offset:0 atIndex:0];
        [computeEncoder setBuffer:persistentCtxBuffer offset:0 atIndex:1];
        [computeEncoder setBuffer:persistentResultBuffer offset:0 atIndex:2];
        [computeEncoder setBytes:&num_nodes length:sizeof(uint32_t) atIndex:3];
        uint32_t baseOffset = 0;
        [computeEncoder setBytes:&baseOffset length:sizeof(uint32_t) atIndex:4];

        NSUInteger threadWidth = resolveResonancePipeline.threadExecutionWidth;
        if (threadWidth == 0) threadWidth = 1;
        NSUInteger threadsPerGroup = threadWidth;
        if (threadsPerGroup > resolveResonancePipeline.maxTotalThreadsPerThreadgroup) {
            threadsPerGroup = resolveResonancePipeline.maxTotalThreadsPerThreadgroup;
        }
        if (threadsPerGroup > num_nodes) threadsPerGroup = num_nodes;
        if (threadsPerGroup == 0) threadsPerGroup = 1;

        MTLSize gridSize = MTLSizeMake(num_nodes, 1, 1);
        MTLSize threadgroupSize = MTLSizeMake(threadsPerGroup, 1, 1);
        [computeEncoder dispatchThreads:gridSize threadsPerThreadgroup:threadgroupSize];
        [computeEncoder endEncoding];

        [commandBuffer commit];
        [commandBuffer waitUntilCompleted];

        if (commandBuffer.status != MTLCommandBufferStatusCompleted) return -5;

        uint64_t* resultPtr = (uint64_t*)[persistentResultBuffer contents];
        *out_result = *resultPtr;

        return 0;
    }
}

int resolve_phasedial_metal(const void* cache_nodes_ptr, uint32_t num_nodes, const void* query_dial_ptr, void* similarities_ptr) {
    if (!resolvePhaseDialPipeline || !cache_nodes_ptr || !query_dial_ptr || !similarities_ptr || num_nodes == 0) return -1;
    if (ensure_phasedial_buffers(num_nodes) != 0) return -2;

    const double* dCache = (const double*)cache_nodes_ptr;
    const double* dQuery = (const double*)query_dial_ptr;

    float* fCache = (float*)[persistentCacheBuf contents];
    for (size_t i = 0; i < (size_t)num_nodes * 1024; i++) fCache[i] = (float)dCache[i];

    float* fQuery = (float*)[persistentQueryBuf contents];
    for (size_t i = 0; i < 1024; i++) fQuery[i] = (float)dQuery[i];

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:persistentCacheBuf offset:0 atIndex:0];
    [enc setBuffer:persistentQueryBuf offset:0 atIndex:1];
    [enc setBuffer:persistentSimBuf offset:0 atIndex:2];
    [enc setBytes:&num_nodes length:sizeof(uint32_t) atIndex:3];
    NSUInteger threads = (NSUInteger)num_nodes * 32;
    int r = runMetalKernel(resolvePhaseDialPipeline, enc, threads, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];

    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) return -4;

    float* simOut = (float*)[persistentSimBuf contents];
    double* dSim = (double*)similarities_ptr;
    for (uint32_t i = 0; i < num_nodes; i++) dSim[i] = (double)simOut[i];

    return 0;
}

int encode_phasedial_metal(const void* structural_phases_ptr, const void* primes_ptr, uint32_t num_values, void* out_dial_ptr) {
    if (!encodePhaseDialPipeline || !structural_phases_ptr || !primes_ptr || !out_dial_ptr) return -1;
    if (ensure_encode_buffers(num_values) != 0) return -2;

    const double* dPhases = (const double*)structural_phases_ptr;
    float* fPhases = (float*)[persistentPhaseBuf contents];
    for (uint32_t i = 0; i < num_values; i++) fPhases[i] = (float)dPhases[i];

    memcpy([persistentPrimeBuf contents], primes_ptr, 512 * sizeof(float));

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:persistentPhaseBuf offset:0 atIndex:0];
    [enc setBuffer:persistentPrimeBuf offset:0 atIndex:1];
    [enc setBuffer:persistentOutDialBuf offset:0 atIndex:2];
    [enc setBytes:&num_values length:sizeof(uint32_t) atIndex:3];
    int r = runMetalKernel(encodePhaseDialPipeline, enc, 512, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];

    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) return -4;

    float* fOut = (float*)[persistentOutDialBuf contents];
    double* dOut = (double*)out_dial_ptr;
    for (size_t i = 0; i < 1024; i++) dOut[i] = (double)fOut[i];

    return 0;
}

int seq_toroidal_mean_phase_metal(const void* value_blocks_ptr, uint32_t num_values, double* out_theta, double* out_phi) {
    if (!seqToroidalMeanPhasePipeline || !value_blocks_ptr || !out_theta || !out_phi || num_values == 0) return -1;
    if (ensure_block_buffers(num_values) != 0) return -2;

    size_t blockBytes = (size_t)num_values * 8 * sizeof(uint64_t);
    memcpy([persistentBlockBuf contents], value_blocks_ptr, blockBytes);

    size_t outBytes = (size_t)num_values * 4 * sizeof(float);
    memset([persistentSeqSumBuf contents], 0, outBytes);

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:persistentBlockBuf offset:0 atIndex:0];
    [enc setBuffer:persistentSeqSumBuf offset:0 atIndex:1];
    [enc setBytes:&num_values length:sizeof(uint32_t) atIndex:2];
    int r = runMetalKernel(seqToroidalMeanPhasePipeline, enc, num_values, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];

    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) return -3;

    float* sums = (float*)[persistentSeqSumBuf contents];
    float sinT = 0, cosT = 0, sinP = 0, cosP = 0;
    for (uint32_t i = 0; i < num_values; i++) {
        sinT += sums[i*4+0];
        cosT += sums[i*4+1];
        sinP += sums[i*4+2];
        cosP += sums[i*4+3];
    }

    *out_theta = atan2((double)sinT, (double)cosT);
    double phi = atan2((double)sinP, (double)cosP);
    if (phi < 0) phi += 2.0 * 3.14159265358979323846;
    *out_phi = phi;

    return 0;
}

int weighted_circular_mean_metal(const void* value_blocks_ptr, uint32_t num_values, double* out_phase, double* out_concentration) {
    if (!weightedCircularMeanPipeline || !value_blocks_ptr || !out_phase || !out_concentration || num_values == 0) return -1;
    if (ensure_block_buffers(num_values) != 0) return -2;

    size_t blockBytes = (size_t)num_values * 8 * sizeof(uint64_t);
    memcpy([persistentBlockBuf contents], value_blocks_ptr, blockBytes);

    size_t outBytes = (size_t)num_values * 3 * sizeof(float);
    memset([persistentWCMSumBuf contents], 0, outBytes);

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:persistentBlockBuf offset:0 atIndex:0];
    [enc setBuffer:persistentWCMSumBuf offset:0 atIndex:1];
    [enc setBytes:&num_values length:sizeof(uint32_t) atIndex:2];
    int r = runMetalKernel(weightedCircularMeanPipeline, enc, num_values, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];

    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) return -3;

    float* sums = (float*)[persistentWCMSumBuf contents];
    float wSin = 0, wCos = 0, wSum = 0;
    for (uint32_t i = 0; i < num_values; i++) {
        wSin += sums[i*3+0];
        wCos += sums[i*3+1];
        wSum += sums[i*3+2];
    }

    *out_phase = atan2((double)wSin, (double)wCos);
    if (*out_phase < 0) *out_phase += 2.0 * 3.14159265358979323846;
    *out_concentration = (wSum > 0) ? (double)(sqrtf(wSin*wSin + wCos*wCos) / wSum) : 0.0;

    return 0;
}

int solve_bvp_metal(const void* start_blocks_ptr, const void* goal_blocks_ptr, uint16_t* out_scale, uint16_t* out_translate, double* out_distance) {
    if (!solveBVPPipeline || !start_blocks_ptr || !goal_blocks_ptr || !out_scale || !out_translate || !out_distance) return -1;
    if (ensure_bvp_buffers() != 0) return -2;

    memcpy([persistentBVPStartBuf contents], start_blocks_ptr, 8 * sizeof(uint64_t));
    memcpy([persistentBVPGoalBuf contents], goal_blocks_ptr, 8 * sizeof(uint64_t));
    uint64_t zero = 0;
    memcpy([persistentBVPResultBuf contents], &zero, sizeof(uint64_t));

    id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
    id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
    [enc setBuffer:persistentBVPStartBuf offset:0 atIndex:0];
    [enc setBuffer:persistentBVPGoalBuf offset:0 atIndex:1];
    [enc setBuffer:persistentBVPResultBuf offset:0 atIndex:2];
    int r = runMetalKernel(solveBVPPipeline, enc, 65792, 256);
    [enc endEncoding];
    [cb commit];
    [cb waitUntilCompleted];

    if (r != 0 || cb.status != MTLCommandBufferStatusCompleted) return -3;

    uint64_t packed = *(uint64_t*)[persistentBVPResultBuf contents];
    uint32_t id = (uint32_t)(packed & 0xFFFFFFFFULL);
    uint32_t match_count = (uint32_t)(packed >> 32);
    *out_scale = (uint16_t)((id / 257) + 1);
    *out_translate = (uint16_t)(id % 257);
    *out_distance = 1.0 - ((double)match_count / 257.0);

    return 0;
}

void cleanup_metal_pools(void) {
    if (persistentNodesBuffer) { [persistentNodesBuffer release]; persistentNodesBuffer = nil; }
    if (persistentCtxBuffer) { [persistentCtxBuffer release]; persistentCtxBuffer = nil; }
    if (persistentResultBuffer) { [persistentResultBuffer release]; persistentResultBuffer = nil; }
    persistentNodesCapacity = 0;

    if (persistentCacheBuf) { [persistentCacheBuf release]; persistentCacheBuf = nil; }
    if (persistentQueryBuf) { [persistentQueryBuf release]; persistentQueryBuf = nil; }
    if (persistentSimBuf) { [persistentSimBuf release]; persistentSimBuf = nil; }
    persistentPhaseDialCapacity = 0;

    if (persistentPhaseBuf) { [persistentPhaseBuf release]; persistentPhaseBuf = nil; }
    if (persistentPrimeBuf) { [persistentPrimeBuf release]; persistentPrimeBuf = nil; }
    if (persistentOutDialBuf) { [persistentOutDialBuf release]; persistentOutDialBuf = nil; }
    persistentEncodeCapacity = 0;

    if (persistentBlockBuf) { [persistentBlockBuf release]; persistentBlockBuf = nil; }
    if (persistentSeqSumBuf) { [persistentSeqSumBuf release]; persistentSeqSumBuf = nil; }
    if (persistentWCMSumBuf) { [persistentWCMSumBuf release]; persistentWCMSumBuf = nil; }
    persistentBlockCapacity = 0;

    if (persistentBVPStartBuf) { [persistentBVPStartBuf release]; persistentBVPStartBuf = nil; }
    if (persistentBVPGoalBuf) { [persistentBVPGoalBuf release]; persistentBVPGoalBuf = nil; }
    if (persistentBVPResultBuf) { [persistentBVPResultBuf release]; persistentBVPResultBuf = nil; }
    persistentBVPAllocated = 0;
}

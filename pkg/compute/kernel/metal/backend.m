//go:build darwin && cgo
// +build darwin,cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include <string.h>
#include "metal.h"
#include "../shared/primitives.h"
#include "../shared/postexec_layout.h"

static id<MTLDevice>       device       = nil;
static id<MTLCommandQueue> commandQueue = nil;

static id<MTLComputePipelineState> pipelineNearestAffinity = nil;
static id<MTLComputePipelineState> pipelineGeometric       = nil;
static id<MTLComputePipelineState> pipelineGeometricIdx    = nil;
static id<MTLComputePipelineState> pipelineBatchFirstFit   = nil;
static id<MTLComputePipelineState> pipelineGossip          = nil;

static dispatch_once_t initOnceToken;
static int initResult = 0;

static id<MTLBuffer> bufArena = nil;
static id<MTLBuffer> bufLinear = nil;

#define VALUE_BYTES 1024

static void releaseInitMetalArenaBuffers(void) {
    if (bufArena) {
        [bufArena release];
        bufArena = nil;
    }
    if (bufLinear) {
        [bufLinear release];
        bufLinear = nil;
    }
}

static id<MTLComputePipelineState> makePipeline(id<MTLLibrary> lib, NSString* name, NSError** err) {
    id<MTLFunction> fn = [lib newFunctionWithName:name];
    if (!fn) { NSLog(@"metal: kernel not found: %@", name); return nil; }
    return [device newComputePipelineStateWithFunction:fn error:err];
}

int count_metal_devices(void) {
    NSArray<id<MTLDevice>> *devices = MTLCopyAllDevices();
    if (!devices) return 0;
    int count = (int)[devices count];
    [devices release];
    return count;
}

int init_metal(const char* metallib_path) {
    if (device != nil && initResult == 0) return 0;

    dispatch_once(&initOnceToken, ^{
        device = MTLCreateSystemDefaultDevice();
        if (!device) { initResult = -1; return; }

        commandQueue = [device newCommandQueue];
        if (!commandQueue) { device = nil; initResult = -2; return; }

        NSString *path = [NSString stringWithUTF8String:metallib_path];
        NSError *error = nil;
        id<MTLLibrary> library = [device newLibraryWithURL:[NSURL fileURLWithPath:path] error:&error];
        if (!library) {
            NSLog(@"metal: failed to load metallib: %@", error);
            commandQueue = nil; device = nil; initResult = -3; return;
        }

        pipelineNearestAffinity = makePipeline(library, @"nearest_affinity_kernel", &error);
        if (!pipelineNearestAffinity) {
            NSLog(@"metal: failed to create nearest_affinity pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -4; return;
        }

        // geometric_kernel (linear grid) and geometric_arena_indices_kernel
        // (arena slots) — both retained; only the arena form is used from Go.
        pipelineGeometric = makePipeline(library, @"geometric_kernel", &error);
        if (!pipelineGeometric) {
            NSLog(@"metal: failed to create geometric pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -5; return;
        }

        pipelineGeometricIdx = makePipeline(library, @"geometric_arena_indices_kernel", &error);
        if (!pipelineGeometricIdx) {
            NSLog(@"metal: failed to create geometric_arena_indices pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -6; return;
        }

        pipelineBatchFirstFit = makePipeline(library, @"batch_first_fit_kernel", &error);
        if (!pipelineBatchFirstFit) {
            NSLog(@"metal: failed to create batch_first_fit pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -7; return;
        }

        pipelineGossip = makePipeline(library, @"hypercube_gossip_kernel", &error);
        if (!pipelineGossip) {
            NSLog(@"metal: failed to create hypercube_gossip pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -8; return;
        }

        initResult = 0;
    });

    return initResult;
}

static void dispatchKernel(id<MTLComputeCommandEncoder> enc,
                           id<MTLComputePipelineState>   pipeline,
                           NSUInteger                    threadCount) {
    [enc setComputePipelineState:pipeline];

    NSUInteger tg = pipeline.threadExecutionWidth;
    NSUInteger maxTg = pipeline.maxTotalThreadsPerThreadgroup;
    if (tg > maxTg) tg = maxTg;
    if (tg > threadCount) tg = threadCount;
    if (tg == 0) tg = 1;

    [enc dispatchThreads:MTLSizeMake(threadCount, 1, 1)
   threadsPerThreadgroup:MTLSizeMake(tg, 1, 1)];
}

static int commitAndWait(id<MTLCommandBuffer> cb) {
    [cb commit];
    [cb waitUntilCompleted];
    return (cb.status == MTLCommandBufferStatusCompleted) ? 0 : -5;
}

int init_metal_arena(void* arena_base, size_t arena_bytes, uint32_t* linear_next_host) {
    if (!device || !arena_base || arena_bytes == 0 || !linear_next_host) return -1;

    releaseInitMetalArenaBuffers();

    bufArena = [device newBufferWithBytesNoCopy:arena_base
                                         length:(NSUInteger)arena_bytes
                                        options:MTLResourceStorageModeShared
                                     deallocator:^(void *pointer, NSUInteger length) {
                                         (void)pointer;
                                         (void)length;
                                     }];
    if (!bufArena) {
        return -2;
    }

    bufLinear = [device newBufferWithBytesNoCopy:linear_next_host
                                          length:sizeof(uint32_t)
                                         options:MTLResourceStorageModeShared
                                      deallocator:^(void *pointer, NSUInteger length) {
                                          (void)pointer;
                                          (void)length;
                                      }];
    if (!bufLinear) {
        releaseInitMetalArenaBuffers();
        return -3;
    }

    return 0;
}

int geometric_metal_indices(const uint32_t* indices, uint32_t count) {
    if (!pipelineGeometricIdx || !indices || count == 0 || !bufArena) return -1;

    NSUInteger idxBytes = (NSUInteger)count * sizeof(uint32_t);
    id<MTLBuffer> idxBuf = [device newBufferWithBytes:indices length:idxBytes options:MTLResourceStorageModeShared];
    if (!idxBuf) return -2;

    @autoreleasepool {
        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setBuffer:bufArena offset:0 atIndex:0];
        [enc setBuffer:idxBuf offset:0 atIndex:1];

        dispatchKernel(enc, pipelineGeometricIdx, (NSUInteger)count);
        [enc endEncoding];

        int r = commitAndWait(cb);
        [idxBuf release];
        return r;
    }
}

int nearest_affinity_metal(void* query_host, void* candidates_host, uint32_t count, uint64_t* best_packed_result) {
    if (!pipelineNearestAffinity || !query_host || !candidates_host || !best_packed_result || count == 0) return -1;

    @autoreleasepool {
        NSUInteger qBytes    = AFFINITY_WORDS * sizeof(uint64_t);
        NSUInteger candBytes = (NSUInteger)count * qBytes;
        NSUInteger resBytes  = sizeof(uint64_t);

        id<MTLBuffer> bufQuery = [device newBufferWithBytes:query_host
                                                     length:qBytes
                                                    options:MTLResourceStorageModeShared];
        id<MTLBuffer> bufCand  = [device newBufferWithBytes:candidates_host
                                                     length:candBytes
                                                    options:MTLResourceStorageModeShared];
        id<MTLBuffer> bufRes   = [device newBufferWithLength:resBytes
                                                     options:MTLResourceStorageModeShared];

        if (!bufQuery || !bufCand || !bufRes) {
            if (bufQuery) [bufQuery release];
            if (bufCand)  [bufCand release];
            if (bufRes)   [bufRes release];
            return -2;
        }

        // Initialize best_packed_result to 0
        memset([bufRes contents], 0, resBytes);

        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setBuffer:bufCand  offset:0 atIndex:0];
        [enc setBuffer:bufQuery offset:0 atIndex:1];
        [enc setBuffer:bufRes   offset:0 atIndex:2];

        dispatchKernel(enc, pipelineNearestAffinity, count);
        [enc endEncoding];

        int r = commitAndWait(cb);

        if (r == 0) {
            memcpy(best_packed_result, [bufRes contents], resBytes);
        }

        [bufQuery release];
        [bufCand release];
        [bufRes release];

        return r;
    }
}

// Mirror of the BatchFirstFitParams struct in backend.metal.
typedef struct {
    uint32_t community_count;
    uint32_t value_count;
    uint32_t hamming_budget;
    uint32_t saturation_cap;
} BatchFirstFitParamsHost;

#define AFFINITY_ROW_WORDS_HOST 8

int batch_first_fit_metal(
    const uint64_t* community_ors_host,
    uint32_t        community_count,
    const uint64_t* value_affinities_host,
    uint32_t        value_count,
    uint32_t        hamming_budget,
    uint32_t        saturation_cap,
    int32_t*        out_host
) {
    if (!pipelineBatchFirstFit || !out_host || value_count == 0) return -1;
    if (community_count > 0 && (!community_ors_host || !value_affinities_host)) return -1;

    @autoreleasepool {
        NSUInteger commBytes = (NSUInteger)community_count * AFFINITY_ROW_WORDS_HOST * sizeof(uint64_t);
        NSUInteger valBytes  = (NSUInteger)value_count     * AFFINITY_ROW_WORDS_HOST * sizeof(uint64_t);
        NSUInteger outBytes  = (NSUInteger)value_count     * sizeof(int32_t);

        // Metal disallows zero-length buffers; allocate a single-row stub
        // when there are no communities so the kernel still launches and
        // emits -1 for every value (matching the CPU/CUDA contract).
        NSUInteger commBufBytes = commBytes;
        if (commBufBytes == 0) commBufBytes = AFFINITY_ROW_WORDS_HOST * sizeof(uint64_t);

        id<MTLBuffer> bufComm = [device newBufferWithLength:commBufBytes
                                                    options:MTLResourceStorageModeShared];
        id<MTLBuffer> bufVal  = [device newBufferWithLength:valBytes
                                                    options:MTLResourceStorageModeShared];
        id<MTLBuffer> bufOut  = [device newBufferWithLength:outBytes
                                                    options:MTLResourceStorageModeShared];

        if (!bufComm || !bufVal || !bufOut) {
            if (bufComm) [bufComm release];
            if (bufVal)  [bufVal  release];
            if (bufOut)  [bufOut  release];
            return -2;
        }

        if (commBytes > 0) {
            memcpy([bufComm contents], community_ors_host, commBytes);
        }
        memcpy([bufVal contents], value_affinities_host, valBytes);

        BatchFirstFitParamsHost params;
        params.community_count = community_count;
        params.value_count     = value_count;
        params.hamming_budget  = hamming_budget;
        params.saturation_cap  = saturation_cap;

        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setBuffer:bufComm offset:0 atIndex:0];
        [enc setBuffer:bufVal  offset:0 atIndex:1];
        [enc setBytes:&params length:sizeof(params) atIndex:2];
        [enc setBuffer:bufOut  offset:0 atIndex:3];

        dispatchKernel(enc, pipelineBatchFirstFit, (NSUInteger)value_count);
        [enc endEncoding];

        int r = commitAndWait(cb);

        if (r == 0) {
            memcpy(out_host, [bufOut contents], outBytes);
        }

        [bufComm release];
        [bufVal  release];
        [bufOut  release];

        return r;
    }
}

// GossipParamsHost mirrors the Metal-side GossipParams struct; passed
// inline via setBytes so the kernel sees its constant buffer.
typedef struct {
    uint32_t value_count;
    uint32_t d_max;
    uint32_t fold_op;
    uint32_t pad;
} GossipParamsHost;

#define GOSSIP_K_PER_CHUNK_HOST 8

int hypercube_gossip_metal_indices(
    const uint32_t* indices,
    uint32_t        value_count,
    uint32_t        d_max,
    uint32_t        fold_op
) {
    if (!pipelineGossip || !indices || value_count == 0 || !bufArena) return -1;

    NSUInteger maxThreads = pipelineGossip.maxTotalThreadsPerThreadgroup;
    if ((NSUInteger)value_count > maxThreads) return -2;

    NSUInteger idxBytes = (NSUInteger)value_count * sizeof(uint32_t);
    id<MTLBuffer> idxBuf = [device newBufferWithBytes:indices length:idxBytes options:MTLResourceStorageModeShared];
    if (!idxBuf) return -3;

    GossipParamsHost params;
    params.value_count = value_count;
    params.d_max       = d_max;
    params.fold_op     = fold_op;
    params.pad         = 0;

    NSUInteger sharedBytes =
        (NSUInteger)value_count * GOSSIP_K_PER_CHUNK_HOST * sizeof(uint64_t);

    @autoreleasepool {
        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setComputePipelineState:pipelineGossip];
        [enc setBuffer:bufArena offset:0 atIndex:0];
        [enc setBuffer:idxBuf  offset:0 atIndex:1];
        [enc setBytes:&params length:sizeof(params) atIndex:2];
        [enc setThreadgroupMemoryLength:sharedBytes atIndex:0];

        [enc dispatchThreadgroups:MTLSizeMake(1, 1, 1)
            threadsPerThreadgroup:MTLSizeMake((NSUInteger)value_count, 1, 1)];
        [enc endEncoding];

        int r = commitAndWait(cb);
        [idxBuf release];
        return r;
    }
}

void cleanup_metal_pools(void) {
    if (bufArena)       { [bufArena release]; bufArena = nil; }
    if (bufLinear)      { [bufLinear release]; bufLinear = nil; }
}


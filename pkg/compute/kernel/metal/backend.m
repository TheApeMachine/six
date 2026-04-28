//go:build darwin && cgo
// +build darwin,cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"
#include "../shared/primitives.h"
#include "../shared/postexec_layout.h"
#include <string.h>

static id<MTLDevice>       device       = nil;
static id<MTLCommandQueue> commandQueue = nil;

static id<MTLComputePipelineState> pipelineGeometric       = nil;
static id<MTLComputePipelineState> pipelineGeometricIdx    = nil;
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

        // geometric_kernel (linear grid) and geometric_arena_indices_kernel
        // (arena slots) — both retained; only the arena form is used from Go.
        pipelineGeometric = makePipeline(library, @"geometric_kernel", &error);
        if (!pipelineGeometric) {
            NSLog(@"metal: failed to create geometric pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -4; return;
        }

        pipelineGeometricIdx = makePipeline(library, @"geometric_arena_indices_kernel", &error);
        if (!pipelineGeometricIdx) {
            NSLog(@"metal: failed to create geometric_arena_indices pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -5; return;
        }

        pipelineGossip = makePipeline(library, @"hypercube_gossip_kernel", &error);
        if (!pipelineGossip) {
            NSLog(@"metal: failed to create hypercube_gossip pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -6; return;
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

// AstParamsHost mirrors the Metal-side AstParams struct; passed inline via
// setBytes so the kernel sees its constant buffer.
typedef struct {
    uint32_t value_count;
    uint32_t owner_index;
    uint32_t owner_slot;
    uint32_t pad1;
} AstParamsHost;

int hypercube_gossip_metal_indices(
    const uint32_t* indices,
    uint32_t        value_count,
    uint32_t        owner_index,
    uint32_t        owner_slot,
    uint32_t*       stage_indices,
    uint32_t*       stage_count
) {
    if (!pipelineGossip || !indices || value_count == 0 || !bufArena || !stage_indices || !stage_count) return -1;

    NSUInteger maxThreads = pipelineGossip.maxTotalThreadsPerThreadgroup;
    if ((NSUInteger)value_count > maxThreads) return -2;

    NSUInteger idxBytes = (NSUInteger)value_count * sizeof(uint32_t);
    id<MTLBuffer> idxBuf = [device newBufferWithBytes:indices length:idxBytes options:MTLResourceStorageModeShared];
    if (!idxBuf) return -3;

    id<MTLBuffer> stageIdxBuf = [device newBufferWithBytes:stage_indices length:idxBytes options:MTLResourceStorageModeShared];
    if (!stageIdxBuf) { [idxBuf release]; return -4; }

    id<MTLBuffer> stageCountBuf = [device newBufferWithBytes:stage_count length:sizeof(uint32_t) options:MTLResourceStorageModeShared];
    if (!stageCountBuf) { [idxBuf release]; [stageIdxBuf release]; return -5; }

    AstParamsHost params;
    params.value_count = value_count;
    params.owner_index = owner_index;
    params.owner_slot  = owner_slot;
    params.pad1        = 0;

    @autoreleasepool {
        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setComputePipelineState:pipelineGossip];
        [enc setBuffer:bufArena offset:0 atIndex:0];
        [enc setBuffer:idxBuf  offset:0 atIndex:1];
        [enc setBytes:&params length:sizeof(params) atIndex:2];
        [enc setBuffer:stageIdxBuf offset:0 atIndex:3];
        [enc setBuffer:stageCountBuf offset:0 atIndex:4];

        [enc dispatchThreadgroups:MTLSizeMake(1, 1, 1)
            threadsPerThreadgroup:MTLSizeMake((NSUInteger)value_count, 1, 1)];
        [enc endEncoding];

        int r = commitAndWait(cb);
        memcpy(stage_indices, [stageIdxBuf contents], idxBytes);
        memcpy(stage_count, [stageCountBuf contents], sizeof(uint32_t));
        [idxBuf release];
        [stageIdxBuf release];
        [stageCountBuf release];
        return r;
    }
}

void cleanup_metal_pools(void) {
    if (bufArena)       { [bufArena release]; bufArena = nil; }
    if (bufLinear)      { [bufLinear release]; bufLinear = nil; }
}

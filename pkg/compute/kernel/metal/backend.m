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

static id<MTLComputePipelineState> pipelineUnifiedBitwise  = nil;
static id<MTLComputePipelineState> pipelineUnifiedArenaIdx = nil;
static id<MTLComputePipelineState> pipelineNearestAffinity = nil;
static id<MTLComputePipelineState> pipelineGeometric       = nil;
static id<MTLComputePipelineState> pipelineGeometricIdx    = nil;

static dispatch_once_t initOnceToken;
static int initResult = 0;

static id<MTLBuffer> bufArena = nil;
static id<MTLBuffer> bufLinear = nil;
static id<MTLBuffer> bufSpawnParent = nil;
static id<MTLBuffer> bufSpawnChild = nil;
static id<MTLBuffer> bufSpawnTail = nil;
static id<MTLBuffer> bufMaxSlots = nil;

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
    if (bufSpawnParent) {
        [bufSpawnParent release];
        bufSpawnParent = nil;
    }
    if (bufSpawnChild) {
        [bufSpawnChild release];
        bufSpawnChild = nil;
    }
    if (bufSpawnTail) {
        [bufSpawnTail release];
        bufSpawnTail = nil;
    }
    if (bufMaxSlots) {
        [bufMaxSlots release];
        bufMaxSlots = nil;
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

        pipelineUnifiedBitwise = makePipeline(library, @"unified_bitwise_kernel", &error);
        if (!pipelineUnifiedBitwise) {
            NSLog(@"metal: failed to create unified_bitwise pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -4; return;
        }

        pipelineUnifiedArenaIdx = makePipeline(library, @"unified_bitwise_arena_indices_kernel", &error);
        if (!pipelineUnifiedArenaIdx) {
            NSLog(@"metal: failed to create unified_bitwise_arena_indices pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -5; return;
        }

        pipelineNearestAffinity = makePipeline(library, @"nearest_affinity_kernel", &error);
        if (!pipelineNearestAffinity) {
            NSLog(@"metal: failed to create nearest_affinity pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -6; return;
        }

        pipelineGeometric = makePipeline(library, @"geometric_kernel", &error);
        if (!pipelineGeometric) {
            NSLog(@"metal: failed to create geometric pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -7; return;
        }

        pipelineGeometricIdx = makePipeline(library, @"geometric_arena_indices_kernel", &error);
        if (!pipelineGeometricIdx) {
            NSLog(@"metal: failed to create geometric_arena_indices pipeline: %@", error);
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

    NSUInteger spawnBytes = (NSUInteger)SPAWN_QUEUE_CAP * sizeof(uint32_t);
    bufSpawnParent = [device newBufferWithLength:spawnBytes options:MTLResourceStorageModeShared];
    bufSpawnChild  = [device newBufferWithLength:spawnBytes options:MTLResourceStorageModeShared];
    bufSpawnTail   = [device newBufferWithLength:sizeof(uint32_t) options:MTLResourceStorageModeShared];
    if (!bufSpawnParent || !bufSpawnChild || !bufSpawnTail) {
        releaseInitMetalArenaBuffers();
        return -4;
    }

    *(uint32_t*)[bufSpawnTail contents] = 0;

    bufMaxSlots = [device newBufferWithLength:sizeof(uint32_t) options:MTLResourceStorageModeShared];
    if (!bufMaxSlots) {
        releaseInitMetalArenaBuffers();
        return -5;
    }

    return 0;
}

int unified_bitwise_metal_indices(const uint32_t* indices, uint32_t count, uint32_t max_slots) {
    if (!pipelineUnifiedArenaIdx || !indices || count == 0 || !bufArena || !bufLinear || !bufSpawnParent) return -1;

    *(uint32_t*)[bufSpawnTail contents] = 0;

    *(uint32_t*)[bufMaxSlots contents] = max_slots;

    NSUInteger idxBytes = (NSUInteger)count * sizeof(uint32_t);
    id<MTLBuffer> idxBuf = [device newBufferWithBytes:indices length:idxBytes options:MTLResourceStorageModeShared];
    if (!idxBuf) return -2;

    @autoreleasepool {
        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setBuffer:bufArena offset:0 atIndex:0];
        [enc setBuffer:idxBuf offset:0 atIndex:1];
        [enc setBuffer:bufLinear offset:0 atIndex:2];
        [enc setBuffer:bufSpawnParent offset:0 atIndex:3];
        [enc setBuffer:bufSpawnChild offset:0 atIndex:4];
        [enc setBuffer:bufSpawnTail offset:0 atIndex:5];
        [enc setBuffer:bufMaxSlots offset:0 atIndex:6];

        dispatchKernel(enc, pipelineUnifiedArenaIdx, (NSUInteger)count);
        [enc endEncoding];

        int r = commitAndWait(cb);
        [idxBuf release];
        return r;
    }
}

int geometric_metal_indices(const uint32_t* indices, uint32_t count, uint32_t max_slots) {
    (void)max_slots;
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

int nearest_affinity_metal(void* query_host, void* candidates_host, uint32_t count, uint32_t* distances_host) {
    if (!pipelineNearestAffinity || !query_host || !candidates_host || !distances_host || count == 0) return -1;

    @autoreleasepool {
        NSUInteger qBytes    = AFFINITY_WORDS * sizeof(uint64_t);
        NSUInteger candBytes = (NSUInteger)count * qBytes;
        NSUInteger distBytes = (NSUInteger)count * sizeof(uint32_t);

        id<MTLBuffer> bufQuery = [device newBufferWithBytes:query_host
                                                     length:qBytes
                                                    options:MTLResourceStorageModeShared];
        id<MTLBuffer> bufCand  = [device newBufferWithBytes:candidates_host
                                                     length:candBytes
                                                    options:MTLResourceStorageModeShared];
        id<MTLBuffer> bufDist  = [device newBufferWithLength:distBytes
                                                     options:MTLResourceStorageModeShared];

        if (!bufQuery || !bufCand || !bufDist) {
            if (bufQuery) [bufQuery release];
            if (bufCand)  [bufCand release];
            if (bufDist)  [bufDist release];
            return -2;
        }

        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setBuffer:bufCand  offset:0 atIndex:0];
        [enc setBuffer:bufQuery offset:0 atIndex:1];
        [enc setBuffer:bufDist  offset:0 atIndex:2];

        dispatchKernel(enc, pipelineNearestAffinity, count);
        [enc endEncoding];

        int r = commitAndWait(cb);

        if (r == 0) {
            memcpy(distances_host, [bufDist contents], distBytes);
        }

        [bufQuery release];
        [bufCand release];
        [bufDist release];

        return r;
    }
}

int metal_drain_spawn_queue(
    uint32_t* parents,
    uint32_t* children,
    uint32_t max_out,
    uint32_t* out_count,
    uint32_t* total_count
) {
    if (!parents || !children || !out_count || !bufSpawnParent || !bufSpawnChild || !bufSpawnTail) {
        return -1;
    }

    uint32_t* tailHost = (uint32_t*)[bufSpawnTail contents];
    uint32_t n = *tailHost;
    if (n > SPAWN_QUEUE_CAP) {
        n = SPAWN_QUEUE_CAP;
    }

    if (total_count) {
        *total_count = n;
    }

    uint32_t copy = n;
    if (copy > max_out) {
        copy = max_out;
    }

    uint32_t* parentContents = (uint32_t*)[bufSpawnParent contents];
    uint32_t* childContents = (uint32_t*)[bufSpawnChild contents];

    memcpy(parents, parentContents, (size_t)copy * sizeof(uint32_t));
    memcpy(children, childContents, (size_t)copy * sizeof(uint32_t));

    if (n > copy) {
        size_t remain = (size_t)(n - copy) * sizeof(uint32_t);
        memmove(parentContents, parentContents + copy, remain);
        memmove(childContents, childContents + copy, remain);
    }

    *tailHost = n - copy;
    *out_count = copy;

    return 0;
}

void cleanup_metal_pools(void) {
    if (bufArena)       { [bufArena release]; bufArena = nil; }
    if (bufLinear)      { [bufLinear release]; bufLinear = nil; }
    if (bufSpawnParent) { [bufSpawnParent release]; bufSpawnParent = nil; }
    if (bufSpawnChild)  { [bufSpawnChild release]; bufSpawnChild = nil; }
    if (bufSpawnTail)   { [bufSpawnTail release]; bufSpawnTail = nil; }
    if (bufMaxSlots)    { [bufMaxSlots release]; bufMaxSlots = nil; }
}

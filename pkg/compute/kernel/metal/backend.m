//go:build darwin && cgo
// +build darwin,cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"
#include "../shared/primitives.h"

static id<MTLDevice>       device       = nil;
static id<MTLCommandQueue> commandQueue = nil;

static id<MTLComputePipelineState> pipelineUnifiedBitwise  = nil;
static id<MTLComputePipelineState> pipelineNearestAffinity = nil;

static dispatch_once_t initOnceToken;
static int initResult = 0;

#define VALUE_BYTES 1024

/*
Single shared buffer pool: one host-visible buffer (poolA) that grows
geometrically (capacity tracked in poolCap) and is reused by every kernel path.
*/
static id<MTLBuffer> poolA   = nil;
static uint32_t      poolCap = 0;

static int ensure_pool(uint32_t num_values) {
    if (poolA != nil && poolCap >= num_values) return 0;

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    if (poolA)   { [poolA release];   poolA   = nil; }

    NSUInteger bytes = (NSUInteger)cap * VALUE_BYTES;

    poolA   = [device newBufferWithLength:bytes options:MTLResourceStorageModeShared];

    if (!poolA) { poolCap = 0; return -1; }

    poolCap = cap;
    return 0;
}

/*
Device init
*/
int count_metal_devices(void) {
    NSArray<id<MTLDevice>> *devices = MTLCopyAllDevices();
    if (!devices) return 0;
    int count = (int)[devices count];
    [devices release];
    return count;
}

static id<MTLComputePipelineState> makePipeline(id<MTLLibrary> lib, NSString* name, NSError** err) {
    id<MTLFunction> fn = [lib newFunctionWithName:name];
    if (!fn) { NSLog(@"metal: kernel not found: %@", name); return nil; }
    return [device newComputePipelineStateWithFunction:fn error:err];
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

        pipelineNearestAffinity = makePipeline(library, @"nearest_affinity_kernel", &error);

        if (!pipelineNearestAffinity) {
            NSLog(@"metal: failed to create nearest_affinity pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -5; return;
        }

        initResult = 0;
    });

    return initResult;
}

/*
Dispatch helpers
*/
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

/*
Unified Bitwise dispatch — each Value carries its own 32-bit in-band program
and the kernel executes the self-only slot sweep directly from the frame.
*/
int unified_bitwise_metal(void* a_host, uint32_t num_values) {
    if (!pipelineUnifiedBitwise || !a_host) return -1;
    if (ensure_pool(num_values) != 0) return -1;

    @autoreleasepool {
        NSUInteger reqBytes = (NSUInteger)num_values * VALUE_BYTES;
        NSUInteger la = [poolA length];
        if (la < reqBytes) {
            NSLog(@"metal: pool buffer too small (need %lu): poolA=%lu",
                  (unsigned long)reqBytes, (unsigned long)la);
            return -6;
        }

        memcpy([poolA contents], a_host, reqBytes);

        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setBuffer:poolA   offset:0 atIndex:0];

        dispatchKernel(enc, pipelineUnifiedBitwise, num_values);
        [enc endEncoding];

        int r = commitAndWait(cb);
        if (r != 0) return r;

        memcpy(a_host, [poolA contents], reqBytes);
        return 0;
    }
}

/*
NearestAffinity dispatch — computes Hamming distances from one query
vector to count candidate vectors, writing uint32 distances to the
caller's buffer. The host performs the argmin reduction.
*/
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

void cleanup_metal_pools(void) {
    if (poolA)   { [poolA release];   poolA   = nil; }
    poolCap = 0;
}

//go:build darwin && cgo
// +build darwin,cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"

static id<MTLDevice>       device       = nil;
static id<MTLCommandQueue> commandQueue = nil;

static id<MTLComputePipelineState> pipelineUniversalBitwise = nil;

static dispatch_once_t initOnceToken;
static int initResult = 0;

#define VALUE_BYTES 1024

/*
Single shared buffer pool
Three buffers (A, B, dst) that grow geometrically. Every kernel uses
the same triple — no per-kernel pool overhead.
*/
static id<MTLBuffer> poolA   = nil;
static id<MTLBuffer> poolB   = nil;
static id<MTLBuffer> poolDst = nil;
static uint32_t      poolCap = 0;

static int ensure_pool(uint32_t num_values) {
    if (poolA != nil && poolCap >= num_values) return 0;

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    if (poolA)   { [poolA release];   poolA   = nil; }
    if (poolB)   { [poolB release];   poolB   = nil; }
    if (poolDst) { [poolDst release]; poolDst = nil; }

    NSUInteger bytes = (NSUInteger)cap * VALUE_BYTES;

    poolA   = [device newBufferWithLength:bytes options:MTLResourceStorageModeShared];
    poolB   = [device newBufferWithLength:bytes options:MTLResourceStorageModeShared];
    poolDst = [device newBufferWithLength:bytes options:MTLResourceStorageModeShared];

    if (!poolA || !poolB || !poolDst) { poolCap = 0; return -1; }

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

        pipelineUniversalBitwise = makePipeline(library, @"universal_bitwise_kernel", &error);

        if (!pipelineUniversalBitwise) {
            NSLog(@"metal: failed to create universal_bitwise pipeline: %@", error);
            commandQueue = nil; device = nil; initResult = -4; return;
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
Universal Bitwise dispatch
*/
int universal_bitwise_metal(const void* a_host, const void* b_host, void* dst_host, uint8_t op, uint32_t num_values) {
    if (!pipelineUniversalBitwise || !a_host || !b_host || !dst_host) return -1;
    if (num_values == 0) return 0;
    if (ensure_pool(num_values) != 0) return -2;

    @autoreleasepool {
        size_t bytes = (size_t)num_values * VALUE_BYTES;
        memcpy([poolA contents], a_host, bytes);
        memcpy([poolB contents], b_host, bytes);

        id<MTLCommandBuffer> cb = [commandQueue commandBuffer];
        id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];

        [enc setBuffer:poolA   offset:0 atIndex:0];
        [enc setBuffer:poolB   offset:0 atIndex:1];
        [enc setBuffer:poolDst offset:0 atIndex:2];
        [enc setBytes:&op length:sizeof(uint8_t) atIndex:3];

        dispatchKernel(enc, pipelineUniversalBitwise, num_values);
        [enc endEncoding];

        int r = commitAndWait(cb);
        if (r != 0) return r;

        memcpy(dst_host, [poolDst contents], bytes);
        return 0;
    }
}

void cleanup_metal_pools(void) {
    if (poolA)   { [poolA release];   poolA   = nil; }
    if (poolB)   { [poolB release];   poolB   = nil; }
    if (poolDst) { [poolDst release]; poolDst = nil; }
    poolCap = 0;
}

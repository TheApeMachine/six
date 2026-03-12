//go:build darwin && cgo
// +build darwin,cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"

// Globals
static id<MTLDevice> device = nil;
static id<MTLCommandQueue> commandQueue = nil;
static id<MTLComputePipelineState> resolveResonancePipeline = nil;

static dispatch_once_t initOnceToken;
static int initResult = 0;

int init_metal(const char* metallib_path) {
    if (device != nil) return 0; // Already initialized

    dispatch_once(&initOnceToken, ^{
        device = MTLCreateSystemDefaultDevice();
        if (!device) { initResult = -1; return; }

        commandQueue = [device newCommandQueue];
        if (!commandQueue) { initResult = -2; return; }

        NSString *path = [NSString stringWithUTF8String:metallib_path];
        NSError *error = nil;
        NSURL *url = [NSURL fileURLWithPath:path];
        id<MTLLibrary> library = [device newLibraryWithURL:url error:&error];
        if (!library) {
            NSLog(@"Failed to load metallib: %@", error);
            initResult = -3;
            return;
        }

        id<MTLFunction> function = [library newFunctionWithName:@"resolve_resonance"];
        if (!function) {
            NSLog(@"Failed to find resolve_resonance function in metallib");
            initResult = -4;
            return;
        }

        resolveResonancePipeline = [device newComputePipelineStateWithFunction:function error:&error];
        if (!resolveResonancePipeline) {
            NSLog(@"Failed to create compute pipeline state: %@", error);
            initResult = -5;
            return;
        }

        initResult = 0; // Success
    });

    return initResult;
}

int resolve_resonance_metal(const void* graph_nodes_ptr, uint32_t num_nodes, const void* active_context_ptr, uint64_t* out_result) {
    if (!resolveResonancePipeline || out_result == NULL) return -1;
    if (num_nodes == 0) {
        *out_result = 0;
        return 0;
    }

    @autoreleasepool {
        NSUInteger nodeBytes = 4; // 2 * uint16_t (GF257 affine rotation struct)
        NSUInteger totalNodesSize = (NSUInteger)num_nodes * nodeBytes;

        static id<MTLBuffer> cachedCtxBuffer = nil;
        static id<MTLBuffer> cachedResultBuffer = nil;

        if (cachedCtxBuffer == nil) {
            cachedCtxBuffer = [device newBufferWithLength:nodeBytes options:MTLResourceStorageModeShared];
            cachedResultBuffer = [device newBufferWithLength:8 options:MTLResourceStorageModeShared];
        }

        // Copy context
        memcpy([cachedCtxBuffer contents], active_context_ptr, nodeBytes);

        // Reset output buffer
        uint64_t initial_val = 0;
        memcpy([cachedResultBuffer contents], &initial_val, 8);

        id<MTLCommandBuffer> commandBuffer = [commandQueue commandBuffer];
        if (!commandBuffer) {
            NSLog(@"Failed to create commandBuffer");
            return -2;
        }

        id<MTLBuffer> nodesBuffer = [device newBufferWithBytesNoCopy:(void*)graph_nodes_ptr length:totalNodesSize options:MTLResourceStorageModeShared deallocator:nil];
        if (!nodesBuffer) {
            nodesBuffer = [device newBufferWithBytes:graph_nodes_ptr length:totalNodesSize options:MTLResourceStorageModeShared];
        }
        if (!nodesBuffer) {
            NSLog(@"Failed to create nodesBuffer (size=%lu)", (unsigned long)totalNodesSize);
            return -3;
        }

        id<MTLComputeCommandEncoder> computeEncoder = [commandBuffer computeCommandEncoder];
        if (!computeEncoder) {
            [nodesBuffer release];
            return -4;
        }

        [computeEncoder setComputePipelineState:resolveResonancePipeline];

        [computeEncoder setBuffer:nodesBuffer offset:0 atIndex:0];
        [computeEncoder setBuffer:cachedCtxBuffer offset:0 atIndex:1];
        [computeEncoder setBuffer:cachedResultBuffer offset:0 atIndex:2]; 
        [computeEncoder setBytes:&num_nodes length:sizeof(uint32_t) atIndex:3];
        uint32_t base_offset = 0;
        [computeEncoder setBytes:&base_offset length:sizeof(uint32_t) atIndex:4];

        NSUInteger threadGroupSize = resolveResonancePipeline.maxTotalThreadsPerThreadgroup;
        if (threadGroupSize > num_nodes) threadGroupSize = num_nodes;
        if (threadGroupSize == 0) threadGroupSize = 1;

        MTLSize threadgroups = MTLSizeMake((num_nodes + threadGroupSize - 1) / threadGroupSize, 1, 1);
        MTLSize threadsPerThreadgroup = MTLSizeMake(threadGroupSize, 1, 1);

        [computeEncoder dispatchThreadgroups:threadgroups threadsPerThreadgroup:threadsPerThreadgroup];
        [computeEncoder endEncoding];
        
        [nodesBuffer release];

        [commandBuffer commit];
        [commandBuffer waitUntilCompleted];

        uint64_t* result_ptr = (uint64_t*)[cachedResultBuffer contents];
        *out_result = *result_ptr;
        return 0;
    }
}

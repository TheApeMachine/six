//go:build darwin && cgo
// +build darwin,cgo

#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"

static id<MTLDevice> device = nil;
static id<MTLCommandQueue> commandQueue = nil;
static id<MTLComputePipelineState> resolveResonancePipeline = nil;

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

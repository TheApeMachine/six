#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"

// Globals
static id<MTLDevice> device = nil;
static id<MTLCommandQueue> commandQueue = nil;
static id<MTLComputePipelineState> bestFillPipeline = nil;

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

        id<MTLFunction> function = [library newFunctionWithName:@"bitwise_best_fill"];
        if (!function) {
            NSLog(@"Failed to find bitwise_best_fill function in metallib");
            initResult = -4;
            return;
        }

        bestFillPipeline = [device newComputePipelineStateWithFunction:function error:&error];
        if (!bestFillPipeline) {
            NSLog(@"Failed to create compute pipeline state: %@", error);
            initResult = -5;
            return;
        }

        initResult = 0; // Success
    });

    return initResult;
}

uint64_t bitwise_best_fill_metal(const void* dictionary_ptr, uint32_t num_chords, const void* active_context_ptr) {
    if (!bestFillPipeline) return 0;
    if (num_chords == 0) return 0;

    @autoreleasepool {
        id<MTLCommandBuffer> commandBuffer = [commandQueue commandBuffer];
        if (!commandBuffer) {
            NSLog(@"Failed to create commandBuffer");
            return 0;
        }
        id<MTLComputeCommandEncoder> computeEncoder = [commandBuffer computeCommandEncoder];
        if (!computeEncoder) {
            NSLog(@"Failed to create computeEncoder");
            return 0;
        }

        [computeEncoder setComputePipelineState:bestFillPipeline];

        NSUInteger dictSize = (NSUInteger)num_chords * 64; // Each chord is 8x uint64 = 64 bytes
        id<MTLBuffer> dictBuffer = [device newBufferWithBytesNoCopy:(void*)dictionary_ptr length:dictSize options:MTLResourceStorageModeShared deallocator:nil];
        if (!dictBuffer) {
            // Fallback to copy if noCopy fails due to alignment
            dictBuffer = [device newBufferWithBytes:dictionary_ptr length:dictSize options:MTLResourceStorageModeShared];
        }
        if (!dictBuffer) {
            NSLog(@"Failed to create dictBuffer");
            [computeEncoder endEncoding];
            return 0;
        }
        [computeEncoder setBuffer:dictBuffer offset:0 atIndex:0];

        id<MTLBuffer> ctxBuffer = [device newBufferWithBytes:active_context_ptr length:64 options:MTLResourceStorageModeShared];
        if (!ctxBuffer) {
            NSLog(@"Failed to create ctxBuffer");
            [computeEncoder endEncoding];
            return 0;
        }
        [computeEncoder setBuffer:ctxBuffer offset:0 atIndex:1];

        uint64_t initial_val = 0;
        id<MTLBuffer> resultBuffer = [device newBufferWithBytes:&initial_val length:8 options:MTLResourceStorageModeShared];
        if (!resultBuffer) {
            NSLog(@"Failed to create resultBuffer");
            [computeEncoder endEncoding];
            return 0;
        }
        [computeEncoder setBuffer:resultBuffer offset:0 atIndex:2];

        [computeEncoder setBytes:&num_chords length:sizeof(uint32_t) atIndex:3];

        NSUInteger threadGroupSize = bestFillPipeline.maxTotalThreadsPerThreadgroup;
        if (threadGroupSize > num_chords) threadGroupSize = num_chords;
        if (threadGroupSize == 0) threadGroupSize = 1;
        
        MTLSize threadgroups = MTLSizeMake((num_chords + threadGroupSize - 1) / threadGroupSize, 1, 1);
        MTLSize threadsPerThreadgroup = MTLSizeMake(threadGroupSize, 1, 1);

        [computeEncoder dispatchThreadgroups:threadgroups threadsPerThreadgroup:threadsPerThreadgroup];

        [computeEncoder endEncoding];
        
        [commandBuffer commit];
        [commandBuffer waitUntilCompleted];

        uint64_t* result_ptr = (uint64_t*)[resultBuffer contents];
        uint64_t final_res = *result_ptr;
        return final_res;
    }
}
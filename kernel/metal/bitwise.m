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

uint64_t bitwise_best_fill_metal(const void* dictionary_ptr, uint32_t num_chords, const void* active_context_ptr, const void* expected_reality_ptr, uint32_t target_id, const void* geodesic_lut_ptr) {
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

        NSUInteger dictSize = (NSUInteger)num_chords * 8648; // IcosahedralManifold is 8648 bytes
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

        static id<MTLBuffer> cachedCtxBuffer = nil;
        static id<MTLBuffer> cachedResultBuffer = nil;
        static id<MTLBuffer> cachedExpectedBuffer = nil;
        static id<MTLBuffer> cachedLutBuffer = nil;

        if (cachedCtxBuffer == nil) {
            cachedCtxBuffer = [device newBufferWithLength:8648 options:MTLResourceStorageModeShared];
            cachedResultBuffer = [device newBufferWithLength:8 options:MTLResourceStorageModeShared];
            cachedExpectedBuffer = [device newBufferWithLength:8648 options:MTLResourceStorageModeShared];
            cachedLutBuffer = [device newBufferWithLength:3600 options:MTLResourceStorageModeShared];
        }

        memcpy([cachedCtxBuffer contents], active_context_ptr, 8648);
        [computeEncoder setBuffer:cachedCtxBuffer offset:0 atIndex:1];

        uint64_t initial_val = 0;
        memcpy([cachedResultBuffer contents], &initial_val, 8);
        [computeEncoder setBuffer:cachedResultBuffer offset:0 atIndex:2];

        if (expected_reality_ptr) {
            memcpy([cachedExpectedBuffer contents], expected_reality_ptr, 8648);
            [computeEncoder setBuffer:cachedExpectedBuffer offset:0 atIndex:5];
        } else {
            [computeEncoder setBuffer:cachedCtxBuffer offset:0 atIndex:5];
        }
        
        if (geodesic_lut_ptr) {
            memcpy([cachedLutBuffer contents], geodesic_lut_ptr, 3600);
            [computeEncoder setBuffer:cachedLutBuffer offset:0 atIndex:6];
        }

        [computeEncoder setBytes:&num_chords length:sizeof(uint32_t) atIndex:3];
        [computeEncoder setBytes:&target_id length:sizeof(uint32_t) atIndex:4];

        NSUInteger threadGroupSize = bestFillPipeline.maxTotalThreadsPerThreadgroup;
        if (threadGroupSize > num_chords) threadGroupSize = num_chords;
        if (threadGroupSize == 0) threadGroupSize = 1;
        
        MTLSize threadgroups = MTLSizeMake((num_chords + threadGroupSize - 1) / threadGroupSize, 1, 1);
        MTLSize threadsPerThreadgroup = MTLSizeMake(threadGroupSize, 1, 1);

        [computeEncoder dispatchThreadgroups:threadgroups threadsPerThreadgroup:threadsPerThreadgroup];

        [computeEncoder endEncoding];
        
        [commandBuffer commit];
        [commandBuffer waitUntilCompleted];

        uint64_t* result_ptr = (uint64_t*)[cachedResultBuffer contents];
        uint64_t final_res = *result_ptr;
        return final_res;
    }
}
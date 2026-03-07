#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.h"

// Globals
static id<MTLDevice> device = nil;
static id<MTLCommandQueue> commandQueue = nil;
static id<MTLComputePipelineState> bestFillPipeline = nil;

static dispatch_once_t initOnceToken;
static int initResult = 0;

// Manifold byte size (257-face Fermat cube: header + 5 cubes × 257 faces × 512 bits).
static const NSUInteger MANIFOLD_BYTES = 82248;

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

uint64_t bitwise_best_fill_metal(const void* dictionary_ptr, uint32_t num_chords, const void* active_context_ptr, const void* expected_reality_ptr, const void* expected_precision_ptr, const void* geodesic_lut_ptr) {
    if (!bestFillPipeline) return 0;
    if (num_chords == 0) return 0;

    @autoreleasepool {
        // Query the device's actual maximum buffer length.
        NSUInteger maxBufLen = [device maxBufferLength];
        NSUInteger totalDictSize = (NSUInteger)num_chords * MANIFOLD_BYTES;

        // Compute tile size: max manifolds per tile that fit in maxBufferLength.
        uint32_t maxManifoldsPerTile = (uint32_t)(maxBufLen / MANIFOLD_BYTES);
        if (maxManifoldsPerTile == 0) maxManifoldsPerTile = 1;

        // Determine number of tiles needed.
        uint32_t numTiles = (num_chords + maxManifoldsPerTile - 1) / maxManifoldsPerTile;

        // Pre-allocate shared buffers (context, result, expected, precision, lut).
        // These are the same across all tiles — allocate once, reuse.
        static id<MTLBuffer> cachedCtxBuffer = nil;
        static id<MTLBuffer> cachedResultBuffer = nil;
        static id<MTLBuffer> cachedExpectedBuffer = nil;
        static id<MTLBuffer> cachedPrecisionBuffer = nil;
        static id<MTLBuffer> cachedLutBuffer = nil;

        if (cachedCtxBuffer == nil) {
            cachedCtxBuffer = [device newBufferWithLength:MANIFOLD_BYTES options:MTLResourceStorageModeShared];
            cachedResultBuffer = [device newBufferWithLength:8 options:MTLResourceStorageModeShared];
            cachedExpectedBuffer = [device newBufferWithLength:MANIFOLD_BYTES options:MTLResourceStorageModeShared];
            cachedPrecisionBuffer = [device newBufferWithLength:(5 * 257 * sizeof(uint16_t)) options:MTLResourceStorageModeShared];
            cachedLutBuffer = [device newBufferWithLength:3600 options:MTLResourceStorageModeShared];
        }

        // Copy shared data once.
        memcpy([cachedCtxBuffer contents], active_context_ptr, MANIFOLD_BYTES);

        uint64_t initial_val = 0;
        memcpy([cachedResultBuffer contents], &initial_val, 8);

        if (expected_reality_ptr) {
            memcpy([cachedExpectedBuffer contents], expected_reality_ptr, MANIFOLD_BYTES);
        }
        if (expected_precision_ptr) {
            memcpy([cachedPrecisionBuffer contents], expected_precision_ptr, (5 * 257 * sizeof(uint16_t)));
        }
        if (geodesic_lut_ptr) {
            memcpy([cachedLutBuffer contents], geodesic_lut_ptr, 3600);
        }

        // Single command buffer for ALL tiles — one commit, one sync point.
        id<MTLCommandBuffer> commandBuffer = [commandQueue commandBuffer];
        if (!commandBuffer) {
            NSLog(@"Failed to create commandBuffer");
            return 0;
        }

        for (uint32_t tile = 0; tile < numTiles; tile++) {
            uint32_t tileStart = tile * maxManifoldsPerTile;
            uint32_t tileCount = maxManifoldsPerTile;
            if (tileStart + tileCount > num_chords) {
                tileCount = num_chords - tileStart;
            }

            NSUInteger tileBytes = (NSUInteger)tileCount * MANIFOLD_BYTES;
            const void* tilePtr = (const uint8_t*)dictionary_ptr + (NSUInteger)tileStart * MANIFOLD_BYTES;

            // Create a buffer for this tile's dictionary chunk.
            id<MTLBuffer> dictBuffer = [device newBufferWithBytesNoCopy:(void*)tilePtr length:tileBytes options:MTLResourceStorageModeShared deallocator:nil];
            if (!dictBuffer) {
                // Fallback to copy if noCopy fails due to alignment.
                dictBuffer = [device newBufferWithBytes:tilePtr length:tileBytes options:MTLResourceStorageModeShared];
            }
            if (!dictBuffer) {
                NSLog(@"Failed to create dictBuffer for tile %u (size=%lu)", tile, (unsigned long)tileBytes);
                continue; // Skip this tile, process remaining tiles.
            }

            id<MTLComputeCommandEncoder> computeEncoder = [commandBuffer computeCommandEncoder];
            if (!computeEncoder) {
                [dictBuffer release];
                continue;
            }

            [computeEncoder setComputePipelineState:bestFillPipeline];

            [computeEncoder setBuffer:dictBuffer offset:0 atIndex:0];
            [computeEncoder setBuffer:cachedCtxBuffer offset:0 atIndex:1];
            [computeEncoder setBuffer:cachedResultBuffer offset:0 atIndex:2]; // Shared — atomic_max accumulates
            [computeEncoder setBytes:&tileCount length:sizeof(uint32_t) atIndex:3];
            [computeEncoder setBytes:&tileStart length:sizeof(uint32_t) atIndex:4]; // base_offset for global ID

            if (expected_reality_ptr) {
                [computeEncoder setBuffer:cachedExpectedBuffer offset:0 atIndex:5];
            } else {
                [computeEncoder setBuffer:cachedCtxBuffer offset:0 atIndex:5];
            }
            if (expected_precision_ptr) {
                [computeEncoder setBuffer:cachedPrecisionBuffer offset:0 atIndex:6];
            }
            if (geodesic_lut_ptr) {
                [computeEncoder setBuffer:cachedLutBuffer offset:0 atIndex:7];
            }

            NSUInteger threadGroupSize = bestFillPipeline.maxTotalThreadsPerThreadgroup;
            if (threadGroupSize > tileCount) threadGroupSize = tileCount;
            if (threadGroupSize == 0) threadGroupSize = 1;

            MTLSize threadgroups = MTLSizeMake((tileCount + threadGroupSize - 1) / threadGroupSize, 1, 1);
            MTLSize threadsPerThreadgroup = MTLSizeMake(threadGroupSize, 1, 1);

            [computeEncoder dispatchThreadgroups:threadgroups threadsPerThreadgroup:threadsPerThreadgroup];
            [computeEncoder endEncoding];
            
            [dictBuffer release];
        }

        // Single commit, single sync — all tiles processed on GPU back-to-back.
        [commandBuffer commit];
        [commandBuffer waitUntilCompleted];

        uint64_t* result_ptr = (uint64_t*)[cachedResultBuffer contents];
        uint64_t final_res = *result_ptr;
        return final_res;
    }
}

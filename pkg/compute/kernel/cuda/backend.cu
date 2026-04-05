#include <cuda_runtime.h>
#include <stdint.h>
#include <stdio.h>
#include "../shared/primitives.h"

/*
UniversalBitwise kernel — CUDA implementation.

Per thread (one Value):
  1. Copy A (4 words) and B (4 words) from Tokens region.
  2. Expand B into 16 rotations × 4 words = 64-word surface.
     A is tiled 16 times to match.
  3. Extract one 4-bit opcode per rotation from Program region.
  4. Apply truth table across the full 64-element surface.
  5. Pack low 8 bits of each result into 8-word Signals region.

Token and Program regions are never mutated.
*/

__global__ void unified_bitwise_kernel(uint64_t* A, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    uint32_t base = id * WORDS;

    // Load A (steady) and B (will be rotated).
    uint64_t a[A_WORDS];
    uint64_t b[B_WORDS];
    for (int i = 0; i < A_WORDS; i++) {
        a[i] = A[base + TOKENS_START_WORD + i];
    }
    for (int i = 0; i < B_WORDS; i++) {
        b[i] = A[base + TOKENS_START_WORD + A_WORDS + i];
    }

    // Load program region.
    uint64_t prog[PROGRAM_WORDS];
    for (int i = 0; i < PROGRAM_WORDS; i++) {
        prog[i] = A[base + PROGRAM_START_WORD + i];
    }

    // Expand surfaces, apply truth table, pack signals.
    uint64_t signals[SIGNALS_WORDS];
    for (int i = 0; i < SIGNALS_WORDS; i++) {
        signals[i] = 0;
    }

    for (int rot = 0; rot < NUM_ROTATIONS; rot++) {
        // Extract 4-bit opcode for this rotation.
        int wordIdx = rot / 2;
        int shift = (rot % 2) * 32;
        uint8_t op = (uint8_t)((prog[wordIdx] >> shift) & 0xF);

        // Build masks from truth table bits.
        uint64_t m0 = 0 - (uint64_t)(op & 1);         // bit 0: a=0,b=0
        uint64_t m1 = 0 - (uint64_t)((op >> 1) & 1);  // bit 1: a=1,b=0
        uint64_t m2 = 0 - (uint64_t)((op >> 2) & 1);  // bit 2: a=0,b=1
        uint64_t m3 = 0 - (uint64_t)((op >> 3) & 1);  // bit 3: a=1,b=1

        for (int w = 0; w < A_WORDS; w++) {
            // Apply truth table: result = (~a&~b&m0) | (a&~b&m1) | (~a&b&m2) | (a&b&m3)
            uint64_t av = a[w];
            uint64_t bv = b[w];
            uint64_t result = (~av & ~bv & m0) |
                              ( av & ~bv & m1) |
                              (~av &  bv & m2) |
                              ( av &  bv & m3);

            // Pack low 8 bits into signals.
            int sigIdx = rot * A_WORDS + w;  // 0..63
            int sigWord = sigIdx / 8;
            int sigShift = (sigIdx % 8) * 8;
            signals[sigWord] |= ((result & 0xFFULL) << sigShift);
        }

        // Rotate B left by 8 bits for next rotation.
        for (int w = 0; w < B_WORDS; w++) {
            b[w] = (b[w] << 8) | (b[w] >> 56);
        }
    }

    // Write only the Signals region.
    for (int i = 0; i < SIGNALS_WORDS; i++) {
        A[base + SIGNALS_START_WORD + i] = signals[i];
    }
}

static uint64_t* d_pool_A = nullptr;
static uint32_t pool_capacity = 0;

static int ensure_pool(uint32_t num_values) {
    if (pool_capacity >= num_values) return 0;

    if (d_pool_A) { cudaFree(d_pool_A); d_pool_A = nullptr; }

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    size_t bytes = (size_t)cap * WORDS * sizeof(uint64_t);
    if (cudaMalloc((void**)&d_pool_A, bytes) != cudaSuccess) return -1;

    pool_capacity = cap;
    return 0;
}

extern "C" {

    int cuda_device_count() {
        int count = 0;
        if (cudaGetDeviceCount(&count) != cudaSuccess) return 0;
        return count;
    }

    void cleanup_cuda_pools() {
        if (d_pool_A) { cudaFree(d_pool_A); d_pool_A = nullptr; }
        pool_capacity = 0;
    }

    int unified_bitwise_cuda(
        int device_id,
        void* a_host,
        uint32_t num_values
    ) {
        if (!a_host || num_values == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_pool(num_values) != 0) return -1;

        size_t bytes = (size_t)num_values * WORDS * sizeof(uint64_t);

        cudaError_t cpyErr = cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaMemcpy H->D failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -4;
        }

        const int threadsPerBlock = 256;
        int blocks = (int)((num_values + threadsPerBlock - 1) / threadsPerBlock);
        if (blocks < 1) blocks = 1;

        unified_bitwise_kernel<<<blocks, threadsPerBlock>>>((uint64_t*)d_pool_A, num_values);

        if (cudaGetLastError()      != cudaSuccess) return -2;
        if (cudaDeviceSynchronize() != cudaSuccess) return -3;

        cpyErr = cudaMemcpy(a_host, d_pool_A, bytes, cudaMemcpyDeviceToHost);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaMemcpy D->H failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -6;
        }
        return 0;
    }
}

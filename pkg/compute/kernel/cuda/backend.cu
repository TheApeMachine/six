#include <cuda_runtime.h>
#include <stdint.h>
#include <stdio.h>
#include "../shared/primitives.h"

__global__ void unified_bitwise_kernel(uint64_t* A, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    uint32_t base = id * WORDS;
    uint64_t ctx[WORDS];
    for (int i = 0; i < WORDS; i++) {
        ctx[i] = A[base + i];
    }

    for (uint32_t slot = 0; slot < MAX_PC; slot++) {
        uint32_t word_pos = PROGRAM_INDEX_WORD + (slot / 2);
        if (word_pos >= WORDS) {
            break;
        }

        uint32_t shift = (slot % 2) * 32;
        uint32_t instr = (uint32_t)(ctx[word_pos] >> shift);
        if (instr == 0) {
            continue;
        }

        uint8_t op = instr & 0xF;
        uint32_t src_word = ((instr >> 4) & 0x3FFF) & 127;
        uint32_t dst_word = ((instr >> 18) & 0x3FFF) & 127;

        uint64_t src = ctx[src_word];
        uint64_t dst = ctx[dst_word];

        uint64_t m0 = 0 - (uint64_t)((op >> 3) & 1);
        uint64_t m1 = 0 - (uint64_t)((op >> 2) & 1);
        uint64_t m2 = 0 - (uint64_t)((op >> 1) & 1);
        uint64_t m3 = 0 - (uint64_t)(op & 1);

        ctx[dst_word] = m0 ^
            ((m0 ^ m2) & src) ^
            ((m0 ^ m1) & dst) ^
            ((m0 ^ m1 ^ m2 ^ m3) & (src & dst));
    }

    for (int i = 0; i < WORDS; i++) {
        A[base + i] = ctx[i];
    }
}

static uint64_t* d_pool_A = nullptr;
static uint32_t pool_capacity = 0;

static int ensure_pool(uint32_t num_values) {
    if (pool_capacity >= num_values) return 0;

    if (d_pool_A) { cudaFree(d_pool_A); d_pool_A = nullptr; }

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    size_t bytes = cap * 1024; // 1024 bytes per Value
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

        size_t bytes = (size_t)num_values * 1024;

        cudaError_t cpyErr = cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaMemcpy H->D d_pool_A failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -4;
        }

        const int threadsPerBlock = 256;
        int blocks = (int)((num_values + threadsPerBlock - 1) / threadsPerBlock);
        if (blocks < 1) blocks = 1;

        unified_bitwise_kernel<<<blocks, threadsPerBlock>>>((uint64_t*)d_pool_A, num_values);

        if (cudaGetLastError()    != cudaSuccess) return -2;
        if (cudaDeviceSynchronize() != cudaSuccess) return -3;

        cpyErr = cudaMemcpy(a_host, d_pool_A, bytes, cudaMemcpyDeviceToHost);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaMemcpy D->H d_pool_A failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -6;
        }
        return 0;
    }
}


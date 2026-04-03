#include <cuda_runtime.h>
#include <stdint.h>
#include <stdio.h>
#include "../shared/primitives.h"

__device__ static uint64_t rotl64(uint64_t x, int r) {
    r &= 63;
    return (x << r) | (x >> (64 - r));
}

__device__ static uint64_t majority_u64(uint64_t a, uint64_t b, uint64_t c) {
    uint64_t out = 0;
    for (int bit = 0; bit < 64; bit++) {
        uint64_t m = 1ULL << bit;
        int cnt = ((a & m) != 0) + ((b & m) != 0) + ((c & m) != 0);
        if (cnt >= 2) {
            out |= m;
        }
    }
    return out;
}

__device__ static void execute_extended_slot(uint64_t* ctx, uint32_t instr) {
    if ((instr & 0x80000000u) == 0u) {
        return;
    }
    uint32_t op = instr & 0x7Fu;
    int argA = (int)((instr >> 7) & 0x7Fu);
    int argB = (int)((instr >> 14) & 0x7Fu);
    int argC = (int)((instr >> 21) & 0x7Fu);

    switch (op) {
    case 1u:
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int wi = TOKENS_START_WORD + i;
            int ai = argA + i;
            ctx[wi] ^= ctx[ai];
        }
        break;
    case 2u:
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int wi = TOKENS_START_WORD + i;
            int ai = argA + i;
            int bi = argB + i;
            ctx[wi] = majority_u64(ctx[wi], ctx[ai], ctx[bi]);
        }
        break;
    case 3u: {
        int rot = argC & 63;
        if (rot == 0) {
            break;
        }
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int wi = TOKENS_START_WORD + i;
            ctx[wi] = rotl64(ctx[wi], rot);
        }
        break;
    }
    case 4u: {
        if (TOKEN_WORDS <= 1) {
            break;
        }
        int sh = argC % TOKEN_WORDS;
        if (sh == 0) {
            break;
        }
        uint64_t buf[128];
        for (int i = 0; i < TOKEN_WORDS; i++) {
            buf[i] = ctx[TOKENS_START_WORD + i];
        }
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int target = (i + sh) % TOKEN_WORDS;
            ctx[TOKENS_START_WORD + target] = buf[i];
        }
        break;
    }
    case 5u:
        ctx[STATE_INDEX_WORD] = 0x4D4D4C44ULL;
        ctx[STATE_ACCUM_WORD] =
            (uint64_t)(argA & 0x7F) | ((uint64_t)(argB & 0x7F) << 8);
        break;
    case 6u:
        /* RESONATOR_UNBIND: macro graph lives on host; GPU path is a no-op. */
        break;
    default:
        break;
    }
}

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

        if (instr & 0x80000000u) {
            execute_extended_slot(ctx, instr);
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


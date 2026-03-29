#include <cuda_runtime.h>
#include <stdint.h>
#include <stdio.h>
#include "../shared/primitives.h"

/* Upper bound on span-loop iterations (matches Metal MAX_SPAN in backend.metal). */
#define MAX_SPAN_ITERATIONS 1048576ULL

#define MARK_EXEC_EXIT(ctx, code) do { \
    (ctx)[EXEC_STATUS_WORD] &= 0x0000FFFFFFFFFFFFULL; \
    (ctx)[EXEC_STATUS_WORD] |= ((uint64_t)(code) << EXEC_STATUS_SHIFT); \
} while (0)

__global__ void unified_bitwise_kernel(
    uint64_t* A,
    const uint64_t* B,
    uint32_t num_values
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    uint32_t base = id * WORDS;

    uint64_t contexts[2][WORDS];
    for (int i = 0; i < WORDS; i++) {
        contexts[0][i] = A[base + i];
        contexts[1][i] = B[base + i];
    }

    contexts[0][EXEC_STATUS_WORD] &= 0x0000FFFFFFFFFFFFULL;

    while (true) {
        uint64_t pc = contexts[0][REG_PC];
        if (pc >= MAX_PC) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_EXHAUSTED);
            break;
        }

        uint64_t wordPos = PROGRAM_INDEX_WORD + (pc / 2);
        if (wordPos >= WORDS) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_BAD_WORD);
            break;
        }
        uint32_t shift = (pc % 2) * 32;
        uint32_t instr = (uint32_t)(contexts[0][wordPos] >> shift);

        uint8_t op = instr & 0xF;
        if (instr == 0) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_HALT);
            break;
        }

        uint16_t sc = (instr >> 4) & 0x3FFF;
        uint16_t dc = (instr >> 18) & 0x3FFF;
        bool sSp = (sc & 0x3F80) == 0x3000;
        bool dSp = (dc & 0x3F80) == 0x3000;

        contexts[0][REG_PC]++;

        if (sSp && dSp) {
            uint64_t sB = (uint64_t)(sc & 0x7F);
            uint64_t dB = (uint64_t)(dc & 0x7F);
            if (sB + 2 >= WORDS || dB + 2 >= WORDS) {
                continue;
            }

            uint64_t sL = contexts[0][sB] & 1ULL;
            uint64_t sS = contexts[0][sB+1];
            uint64_t sE = contexts[0][sB+2];
            uint64_t dL = contexts[0][dB] & 1ULL;
            uint64_t dS = contexts[0][dB+1];
            uint64_t dE = contexts[0][dB+2];
            if (sE <= sS || dE <= dS) {
                continue;
            }

            uint64_t sN = sE - sS;
            uint64_t dN = dE - dS;
            uint64_t limit = (sN < dN) ? sN : dN;
            if (sN == 1) {
                limit = dN;
            }
            if (limit > MAX_SPAN_ITERATIONS) {
                limit = MAX_SPAN_ITERATIONS;
            }
            for (uint64_t i = 0; i < limit; i++) {
                uint64_t sBit = sS + i;
                if (sN == 1) {
                    sBit = sS;
                }
                uint64_t sWord = sBit / 64;
                uint64_t sShift = sBit % 64;
                uint64_t dWord = (dS + i) / 64;
                uint64_t dShift = (dS + i) % 64;
                if (sWord >= WORDS || dWord >= WORDS) {
                    break;
                }

                uint8_t sb = (contexts[sL][sWord] >> sShift) & 1;
                uint8_t db = (contexts[dL][dWord] >> dShift) & 1;

                uint32_t idx = (1 - db) | ((1 - sb) << 1);
                uint8_t res = (op >> idx) & 1;

                if (res == 1) {
                    contexts[dL][dWord] |= (1ULL << dShift);
                } else {
                    contexts[dL][dWord] &= ~(1ULL << dShift);
                }
            }
            continue;
        }

        if (dSp) {
            uint32_t dstIdx = (uint32_t)(dc & 0x7F);
            if (dstIdx >= WORDS) {
                continue;
            }

            uint64_t left = (uint64_t)sc;
            if ((sc & 0x3F80) == 0x3000) {
                uint32_t srcIdx = (uint32_t)(sc & 0x7F);
                if (srcIdx >= WORDS) {
                    continue;
                }
                left = contexts[0][srcIdx];
            }
            uint64_t right = contexts[0][dstIdx];

            uint64_t m0 = 0 - (uint64_t)((op >> 3) & 1);
            uint64_t m1 = 0 - (uint64_t)((op >> 2) & 1);
            uint64_t m2 = 0 - (uint64_t)((op >> 1) & 1);
            uint64_t m3 = 0 - (uint64_t)(op & 1);

            uint64_t res = m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right));
            contexts[0][dstIdx] = res;
        }
    }

    for (int i = 0; i < WORDS; i++) {
        A[base + i] = contexts[0][i];
    }
}

static uint64_t* d_pool_A = nullptr;
static uint64_t* d_pool_B = nullptr;
static uint32_t pool_capacity = 0;

static int ensure_pool(uint32_t num_values) {
    if (pool_capacity >= num_values) return 0;

    if (d_pool_A) { cudaFree(d_pool_A); d_pool_A = nullptr; }
    if (d_pool_B) { cudaFree(d_pool_B); d_pool_B = nullptr; }

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    size_t bytes = cap * 1024; // 1024 bytes per Value
    if (cudaMalloc((void**)&d_pool_A, bytes) != cudaSuccess) return -1;
    if (cudaMalloc((void**)&d_pool_B, bytes) != cudaSuccess) return -1;

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
        if (d_pool_B) { cudaFree(d_pool_B); d_pool_B = nullptr; }
        pool_capacity = 0;
    }

    int unified_bitwise_cuda(
        int device_id,
        void* a_host,
        const void* b_host,
        uint32_t num_values
    ) {
        if (!a_host || !b_host || num_values == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_pool(num_values) != 0) return -1;

        size_t bytes = (size_t)num_values * 1024;

        cudaError_t cpyErr = cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaMemcpy H->D d_pool_A failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -4;
        }
        cpyErr = cudaMemcpy(d_pool_B, b_host, bytes, cudaMemcpyHostToDevice);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaMemcpy H->D d_pool_B failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -5;
        }

        const int threadsPerBlock = 256;
        int blocks = (int)((num_values + threadsPerBlock - 1) / threadsPerBlock);
        if (blocks < 1) blocks = 1;

        unified_bitwise_kernel<<<blocks, threadsPerBlock>>>(
            (uint64_t*)d_pool_A,
            (const uint64_t*)d_pool_B,
            num_values
        );

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

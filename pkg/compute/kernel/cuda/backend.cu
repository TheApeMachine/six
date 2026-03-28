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
    const uint64_t* B
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= 1) return;

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
        if (op == 0 && pc > 0) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_HALT);
            break;
        }

        uint16_t srcCode = (instr >> 4) & 0x3FFF;
        uint16_t dstCode = (instr >> 18) & 0x3FFF;

        contexts[0][REG_PC]++;

        uint64_t srcVal = 0;
        bool sSpan = false;
        if ((srcCode & 0x1000) != 0) {
            srcVal = (uint64_t)(srcCode & 0x0FFF);
            sSpan = true;
        } else if ((srcCode & 0x2000) != 0) {
            uint32_t srcIdx = (uint32_t)(srcCode & 0x0FFF);
            if (srcIdx < WORDS) {
                srcVal = contexts[0][srcIdx];
            } else {
                srcVal = 0;
            }
        } else {
            srcVal = (uint64_t)srcCode;
        }

        uint64_t dstVal = 0;
        bool dSpan = false;
        if ((dstCode & 0x1000) != 0) {
            dstVal = (uint64_t)(dstCode & 0x0FFF);
            dSpan = true;
        } else if ((dstCode & 0x2000) != 0) {
            uint32_t dstIdxReg = (uint32_t)(dstCode & 0x0FFF);
            if (dstIdxReg < WORDS) {
                dstVal = contexts[0][dstIdxReg];
            } else {
                dstVal = 0;
            }
        } else {
            dstVal = (uint64_t)dstCode;
        }

        if (sSpan || dSpan) {
            uint64_t sBase = srcVal;
            uint64_t sCtx = 0, sOff = 0, sLen = 0;
            if (sBase + 2 < WORDS) {
                sCtx = contexts[0][sBase];
                sOff = contexts[0][sBase+1];
                sLen = contexts[0][sBase+2];
            }

            uint64_t dBase = dstVal;
            uint64_t dCtx = 0, dOff = 0, dLen = 0;
            if (dBase + 2 < WORDS) {
                dCtx = contexts[0][dBase];
                dOff = contexts[0][dBase+1];
                dLen = contexts[0][dBase+2];
            }

            uint64_t limit = (sLen < dLen) ? sLen : dLen;
            if (limit > MAX_SPAN_ITERATIONS) {
                limit = MAX_SPAN_ITERATIONS;
            }
            for (uint64_t i = 0; i < limit; i++) {
                uint64_t sWord = (sOff + i) / 64;
                uint64_t sBit = (sOff + i) % 64;
                uint8_t sb = 0;
                if (sWord < WORDS) sb = (contexts[sCtx % 2][sWord] >> sBit) & 1;

                uint64_t dWord = (dOff + i) / 64;
                uint64_t dBit = (dOff + i) % 64;
                uint8_t db = 0;
                if (dWord < WORDS) db = (contexts[dCtx % 2][dWord] >> dBit) & 1;

                uint32_t idx = (1 - db) | ((1 - sb) << 1);
                uint8_t res = (op >> idx) & 1;

                if (dWord < WORDS) {
                    if (res == 1) {
                        contexts[dCtx % 2][dWord] |= (1ULL << dBit);
                    } else {
                        contexts[dCtx % 2][dWord] &= ~(1ULL << dBit);
                    }
                }
            }
            continue;
        }

        uint32_t dstIdx = dstCode & 0x0FFF;
        uint64_t left = srcVal;
        uint64_t right = dstVal;

        uint64_t m0 = 0 - (uint64_t)((op >> 3) & 1);
        uint64_t m1 = 0 - (uint64_t)((op >> 2) & 1);
        uint64_t m2 = 0 - (uint64_t)((op >> 1) & 1);
        uint64_t m3 = 0 - (uint64_t)(op & 1);

        uint64_t res = m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right));
        if (dstIdx < WORDS) {
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
        const void* b_host
    ) {
        if (!a_host || !b_host) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_pool(1) != 0) return -1;

        size_t bytes = 1024;

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

        int threads = 1;
        int blocks  = 1;

        unified_bitwise_kernel<<<blocks, threads>>>(
            (uint64_t*)d_pool_A,
            (const uint64_t*)d_pool_B
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
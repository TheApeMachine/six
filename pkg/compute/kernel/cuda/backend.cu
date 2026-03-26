#include <cuda_runtime.h>
#include <stdint.h>
#include "../shared/primitives.h"

__global__ void unified_bitwise_kernel(
    const uint64_t* A,
    const uint64_t* B,
    uint64_t* DST,
    uint32_t num_values
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    uint32_t base = id * WORDS;

    uint64_t contexts[2][128];
    for (int i = 0; i < WORDS; i++) {
        contexts[0][i] = A[base + i];
        contexts[1][i] = B[base + i];
    }

    while (true) {
        uint64_t pc = contexts[0][REG_PC];
        if (pc >= MAX_PC) break;

        uint64_t wordPos = PROGRAM_INDEX_WORD + (pc / 2);
        uint32_t shift = (pc % 2) * 32;
        uint32_t instr = (uint32_t)(contexts[0][wordPos] >> shift);

        uint8_t op = instr & 0xF;
        if (op == 0 && pc > 0) break;

        uint16_t srcCode = (instr >> 4) & 0x3FFF;
        uint16_t dstCode = (instr >> 18) & 0x3FFF;

        contexts[0][REG_PC]++;

        uint64_t srcVal = 0;
        bool sSpan = false;
        if ((srcCode & 0x1000) != 0) {
            srcVal = (uint64_t)(srcCode & 0x0FFF);
            sSpan = true;
        } else if ((srcCode & 0x2000) != 0) {
            srcVal = contexts[0][srcCode & 0x0FFF];
        } else {
            srcVal = (uint64_t)srcCode;
        }

        uint64_t dstVal = 0;
        bool dSpan = false;
        if ((dstCode & 0x1000) != 0) {
            dstVal = (uint64_t)(dstCode & 0x0FFF);
            dSpan = true;
        } else if ((dstCode & 0x2000) != 0) {
            dstVal = contexts[0][dstCode & 0x0FFF];
        } else {
            dstVal = (uint64_t)dstCode;
        }

        if (sSpan || dSpan) {
            uint64_t sBase = srcVal;
            uint64_t sCtx = contexts[0][sBase];
            uint64_t sOff = contexts[0][sBase+1];
            uint64_t sLen = contexts[0][sBase+2];

            uint64_t dBase = dstVal;
            uint64_t dCtx = contexts[0][dBase];
            uint64_t dOff = contexts[0][dBase+1];
            uint64_t dLen = contexts[0][dBase+2];

            uint64_t limit = (sLen < dLen) ? sLen : dLen;
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
        contexts[0][dstIdx] = res;
    }

    for (int i = 0; i < WORDS; i++) {
        DST[base + i] = contexts[0][i];
    }
}

static uint64_t* d_pool_A = nullptr;
static uint64_t* d_pool_B = nullptr;
static uint64_t* d_pool_dst = nullptr;
static uint32_t pool_capacity = 0;

static int ensure_pool(uint32_t num_values) {
    if (pool_capacity >= num_values) return 0;

    if (d_pool_A) cudaFree(d_pool_A);
    if (d_pool_B) cudaFree(d_pool_B);
    if (d_pool_dst) cudaFree(d_pool_dst);

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    size_t bytes = cap * 1024; // 1024 bytes per Value
    if (cudaMalloc((void**)&d_pool_A, bytes) != cudaSuccess) return -1;
    if (cudaMalloc((void**)&d_pool_B, bytes) != cudaSuccess) return -1;
    if (cudaMalloc((void**)&d_pool_dst, bytes) != cudaSuccess) return -1;

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
        if (d_pool_dst) { cudaFree(d_pool_dst); d_pool_dst = nullptr; }
        pool_capacity = 0;
    }

    int unified_bitwise_cuda(
        int device_id,
        const void* a_host,
        const void* b_host,
        void* dst_host,
        uint32_t num_values
    ) {
        if (!a_host || !b_host || !dst_host || num_values == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_pool(num_values) != 0) return -1;

        size_t bytes = num_values * 1024;

        cudaMemcpy(d_pool_A,   a_host, bytes, cudaMemcpyHostToDevice);
        cudaMemcpy(d_pool_B,   b_host, bytes, cudaMemcpyHostToDevice);

        int threads = 256;
        int blocks  = (num_values + threads - 1) / threads;

        unified_bitwise_kernel<<<blocks, threads>>>(
            (const uint64_t*)d_pool_A,
            (const uint64_t*)d_pool_B,
            (uint64_t*)d_pool_dst,
            num_values
        );

        if (cudaGetLastError()    != cudaSuccess) return -2;
        if (cudaDeviceSynchronize() != cudaSuccess) return -3;

        cudaMemcpy(dst_host, d_pool_dst, bytes, cudaMemcpyDeviceToHost);
        return 0;
    }
}
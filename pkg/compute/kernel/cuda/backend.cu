#include <cuda_runtime.h>
#include <stdint.h>
#include <stdio.h>
#include "../shared/primitives.h"

#define CONTEXT_START_WORD 32
#define GRADIENT_START_WORD 40
#define OPCODE_GEOMETRIC_MASK 0xF0
#define OPCODE_GEOMETRIC_COMPOSE 0x10
#define OPCODE_GEOMETRIC_SANDWICH 0x20
#define OPCODE_GEOMETRIC_REVERSE 0x30
#define OPCODE_REGION_PROGRAM 0x40
#define OPCODE_COPY_MASK_MERGE 0x50
#define RESERVED_START_WORD 56
#define PROGRAM_CONTRACT_SHIFT 8
#define CONTRACT_EXACT_BINARY 1

struct Multivector {
    double v[8];
};

static __device__ __forceinline__ double word_to_double(uint64_t word) {
    return __longlong_as_double((long long)word);
}

static __device__ __forceinline__ uint64_t double_to_word(double value) {
    return (uint64_t)__double_as_longlong(value);
}

static __device__ __forceinline__ Multivector load_multivector(uint64_t* frame, int start) {
    Multivector mv;
    #pragma unroll
    for (int idx = 0; idx < 8; idx++) {
        mv.v[idx] = word_to_double(frame[start + idx]);
    }
    return mv;
}

static __device__ __forceinline__ void store_multivector(uint64_t* frame, int start, Multivector mv) {
    #pragma unroll
    for (int idx = 0; idx < 8; idx++) {
        frame[start + idx] = double_to_word(mv.v[idx]);
    }
}

static __device__ __forceinline__ Multivector geometric_product(Multivector left, Multivector right) {
    Multivector out;
    out.v[0] = left.v[0]*right.v[0] - left.v[4]*right.v[4] - left.v[5]*right.v[5] - left.v[6]*right.v[6];
    out.v[1] = left.v[0]*right.v[1] + left.v[1]*right.v[0] - left.v[2]*right.v[4] + left.v[3]*right.v[5] +
               left.v[4]*right.v[2] - left.v[5]*right.v[3] - left.v[6]*right.v[7] - left.v[7]*right.v[6];
    out.v[2] = left.v[0]*right.v[2] + left.v[1]*right.v[4] + left.v[2]*right.v[0] - left.v[3]*right.v[6] -
               left.v[4]*right.v[1] - left.v[5]*right.v[7] + left.v[6]*right.v[3] - left.v[7]*right.v[5];
    out.v[3] = left.v[0]*right.v[3] - left.v[1]*right.v[5] + left.v[2]*right.v[6] + left.v[3]*right.v[0] -
               left.v[4]*right.v[7] + left.v[5]*right.v[1] - left.v[6]*right.v[2] - left.v[7]*right.v[4];
    out.v[4] = left.v[0]*right.v[4] + left.v[4]*right.v[0] + left.v[5]*right.v[6] - left.v[6]*right.v[5];
    out.v[5] = left.v[0]*right.v[5] - left.v[4]*right.v[6] + left.v[5]*right.v[0] + left.v[6]*right.v[4];
    out.v[6] = left.v[0]*right.v[6] + left.v[4]*right.v[5] - left.v[5]*right.v[4] + left.v[6]*right.v[0];
    out.v[7] = left.v[0]*right.v[7] + left.v[1]*right.v[6] + left.v[2]*right.v[5] + left.v[3]*right.v[4] +
               left.v[4]*right.v[3] + left.v[5]*right.v[2] + left.v[6]*right.v[1] + left.v[7]*right.v[0];
    return out;
}

static __device__ __forceinline__ Multivector reverse(Multivector mv) {
    Multivector out;
    out.v[0] =  mv.v[0];
    out.v[1] = -mv.v[1];
    out.v[2] = -mv.v[2];
    out.v[3] = -mv.v[3];
    out.v[4] = -mv.v[4];
    out.v[5] = -mv.v[5];
    out.v[6] = -mv.v[6];
    out.v[7] =  mv.v[7];
    return out;
}

static __device__ __forceinline__ Multivector sandwich(Multivector motor, Multivector target) {
    return geometric_product(geometric_product(motor, target), reverse(motor));
}

static __device__ __forceinline__ void unpack_region_ref(uint64_t word, int* start, int* span) {
    *start = (int)(uint32_t)word;
    *span = (int)(uint32_t)(word >> 32);
}

static __device__ __forceinline__ uint64_t exact_binary_word(uint64_t op, uint64_t a, uint64_t b) {
    uint64_t m0 = (op & 1) ? ~0ULL : 0ULL;
    uint64_t m1 = (op & 2) ? ~0ULL : 0ULL;
    uint64_t m2 = (op & 4) ? ~0ULL : 0ULL;
    uint64_t m3 = (op & 8) ? ~0ULL : 0ULL;
    return (a & b & m0) |
           (a & ~b & m1) |
           (~a & b & m2) |
           (~a & ~b & m3);
}

static __device__ __forceinline__ void copy_mask_merge_device(uint64_t* frame) {
    int aStart, aSpan, bStart, bSpan, dstStart, dstSpan;
    unpack_region_ref(frame[PROGRAM_START_WORD + 3], &aStart, &aSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 4], &bStart, &bSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 5], &dstStart, &dstSpan);
    int n = aSpan;
    if (bSpan < n) n = bSpan;
    if (dstSpan < n) n = dstSpan;
    if (n <= 0) return;
    if (aStart < 0 || bStart < 0 || dstStart < 0) return;
    if (aStart + n > 128 || bStart + n > 128 || dstStart + n > 128) return;
    for (int idx = 0; idx < n; idx++) {
        uint64_t mask = frame[bStart + idx];
        uint64_t src = frame[aStart + idx];
        int dst = dstStart + idx;
        frame[dst] = (src & mask) | (frame[dst] & ~mask);
    }
}

static __device__ __forceinline__ void exact_binary_device(
    uint64_t* frame,
    uint64_t op,
    int aStart, int aSpan,
    int bStart, int bSpan,
    int dstStart, int dstSpan
) {
    if (aSpan <= 0 || bSpan <= 0 || dstSpan <= 0) return;
    if (aStart < 0 || bStart < 0 || dstStart < 0) return;
    int limit = aSpan;
    if (bSpan < limit) limit = bSpan;
    if (dstSpan < limit) limit = dstSpan;
    if (aStart + limit > 128 || bStart + limit > 128 || dstStart + limit > 128) return;
    for (int idx = 0; idx < limit; idx++) {
        frame[dstStart + idx] = exact_binary_word(op, frame[aStart + idx], frame[bStart + idx]);
    }
}

static __device__ __forceinline__ void universal_bitwise_device(
    uint64_t* frame,
    int aStart, int aSpan,
    int bStart, int bSpan,
    int dstStart, int dstSpan,
    int mode, uint64_t opcodeTable
) {
    if (aSpan <= 0 || bSpan <= 0 || dstSpan <= 0) return;
    if (aStart < 0 || bStart < 0 || dstStart < 0) return;
    if (aStart + aSpan > 128 || bStart + bSpan > 128 || dstStart + dstSpan > 128) return;

    uint64_t aLane[4] = {0, 0, 0, 0};
    for (int idx = 0; idx < aSpan; idx++) {
        aLane[idx & 3] ^= frame[aStart + idx];
    }

    uint8_t sigBytes[64];
    for (int i = 0; i < 64; i++) sigBytes[i] = 0;

    for (int rot = 0; rot < 16; rot++) {
        uint8_t op = (uint8_t)((opcodeTable >> (rot * 4)) & 0xF);
        uint64_t m0 = (op & 1) ? ~0ULL : 0ULL;
        uint64_t m1 = (op & 2) ? ~0ULL : 0ULL;
        uint64_t m2 = (op & 4) ? ~0ULL : 0ULL;
        uint64_t m3 = (op & 8) ? ~0ULL : 0ULL;

        for (int lane = 0; lane < 4; lane++) {
            int bIdx = bStart + ((rot * 4) + lane) % bSpan;
            uint64_t a = aLane[lane];
            uint64_t b = frame[bIdx];
            uint64_t notA = ~a;
            uint64_t notB = ~b;

            uint64_t result = (a & b & m0) |
                              (a & notB & m1) |
                              (notA & b & m2) |
                              (notA & notB & m3);

            sigBytes[rot * 4 + lane] = (uint8_t)(result & 0xFF);
        }
    }

    uint64_t sigWords[8];
    for (int w = 0; w < 8; w++) {
        int base = w * 8;
        sigWords[w] = (uint64_t)sigBytes[base] |
                      ((uint64_t)sigBytes[base + 1] << 8) |
                      ((uint64_t)sigBytes[base + 2] << 16) |
                      ((uint64_t)sigBytes[base + 3] << 24) |
                      ((uint64_t)sigBytes[base + 4] << 32) |
                      ((uint64_t)sigBytes[base + 5] << 40) |
                      ((uint64_t)sigBytes[base + 6] << 48) |
                      ((uint64_t)sigBytes[base + 7] << 56);
    }

    if (mode == 0) {
        int limit = dstSpan;
        if (limit > 8) limit = 8;
        for (int idx = 0; idx < limit; idx++) {
            frame[dstStart + idx] ^= sigWords[idx];
        }
        return;
    }

    uint64_t total = 0;
    for (int idx = 0; idx < 8; idx++) {
        total += __popcll(sigWords[idx]);
    }
    frame[dstStart] = total;
}

static __device__ __forceinline__ uint64_t broadcast_opcode_nibble(uint8_t opLow) {
    uint64_t n = (uint64_t)(opLow & 0xF);
    uint64_t packed = 0;
    for (int i = 0; i < 16; i++) {
        packed |= n << (i * 4);
    }
    return packed;
}

__global__ void unified_bitwise_kernel(uint64_t* A, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    uint32_t base = id * WORDS;
    uint64_t* frame = A + base;

    uint8_t opLow = (uint8_t)(frame[PROGRAM_START_WORD] & 0xF);
    if (opLow == 0) return;

    uint64_t opcodeTable = broadcast_opcode_nibble(opLow);
    int mode = (int)(frame[PROGRAM_START_WORD + 1] & 0xFF);
    int aStart, aSpan, bStart, bSpan, dstStart, dstSpan;
    unpack_region_ref(frame[PROGRAM_START_WORD + 3], &aStart, &aSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 4], &bStart, &bSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 5], &dstStart, &dstSpan);

    universal_bitwise_device(frame, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, mode, opcodeTable);
}

__global__ void nearest_affinity_kernel(
    const uint64_t* candidates,
    const uint64_t* query,
    uint32_t*       distances,
    uint32_t        count
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= count) return;

    uint32_t base = id * AFFINITY_WORDS;
    uint32_t dist = 0;

    for (int w = 0; w < AFFINITY_WORDS; w++) {
        dist += __popcll(candidates[base + w] ^ query[w]);
    }

    distances[id] = dist;
}

__global__ void geometric_kernel(uint64_t* A, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    uint32_t base = id * WORDS;
    uint64_t* frame = A + base;
    uint8_t op = (uint8_t)(frame[PROGRAM_START_WORD] & OPCODE_GEOMETRIC_MASK);

    if (op != OPCODE_GEOMETRIC_COMPOSE &&
        op != OPCODE_GEOMETRIC_SANDWICH &&
        op != OPCODE_GEOMETRIC_REVERSE) {
        return;
    }

    Multivector left = load_multivector(frame, CONTEXT_START_WORD);
    Multivector right = load_multivector(frame, GRADIENT_START_WORD);

    if (op == OPCODE_GEOMETRIC_COMPOSE) {
        store_multivector(frame, SIGNALS_START_WORD, geometric_product(left, right));
        return;
    }

    if (op == OPCODE_GEOMETRIC_SANDWICH) {
        store_multivector(frame, SIGNALS_START_WORD, sandwich(left, right));
        return;
    }

    if (op == OPCODE_GEOMETRIC_REVERSE) {
        store_multivector(frame, SIGNALS_START_WORD, reverse(left));
        return;
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

static uint64_t*  d_aff_candidates = nullptr;
static uint64_t*  d_aff_query      = nullptr;
static uint32_t*  d_aff_distances  = nullptr;
static uint32_t   aff_pool_cap     = 0;

static int ensure_aff_pool(uint32_t count) {
    if (aff_pool_cap >= count) return 0;

    if (d_aff_candidates) { cudaFree(d_aff_candidates); d_aff_candidates = nullptr; }
    if (d_aff_query)      { cudaFree(d_aff_query);      d_aff_query      = nullptr; }
    if (d_aff_distances)  { cudaFree(d_aff_distances);   d_aff_distances  = nullptr; }

    uint32_t cap = count * 2;
    if (cap < 256) cap = 256;

    size_t cand_bytes = (size_t)cap * AFFINITY_WORDS * sizeof(uint64_t);
    size_t dist_bytes = (size_t)cap * sizeof(uint32_t);
    size_t q_bytes    = AFFINITY_WORDS * sizeof(uint64_t);

    if (cudaMalloc((void**)&d_aff_candidates, cand_bytes) != cudaSuccess) return -1;
    if (cudaMalloc((void**)&d_aff_query,      q_bytes)    != cudaSuccess) return -1;
    if (cudaMalloc((void**)&d_aff_distances,  dist_bytes) != cudaSuccess) return -1;

    aff_pool_cap = cap;
    return 0;
}

extern "C" {

    int cuda_device_count() {
        int count = 0;
        if (cudaGetDeviceCount(&count) != cudaSuccess) return 0;
        return count;
    }

    void cleanup_cuda_pools() {
        if (d_pool_A)          { cudaFree(d_pool_A);          d_pool_A          = nullptr; }
        if (d_aff_candidates)  { cudaFree(d_aff_candidates);  d_aff_candidates  = nullptr; }
        if (d_aff_query)       { cudaFree(d_aff_query);       d_aff_query       = nullptr; }
        if (d_aff_distances)   { cudaFree(d_aff_distances);   d_aff_distances   = nullptr; }
        pool_capacity = 0;
        aff_pool_cap  = 0;
    }

    int nearest_affinity_cuda(
        int device_id,
        void* query_host,
        void* candidates_host,
        uint32_t count,
        uint32_t* distances_host
    ) {
        if (!query_host || !candidates_host || !distances_host || count == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_aff_pool(count) != 0) return -1;

        size_t q_bytes    = AFFINITY_WORDS * sizeof(uint64_t);
        size_t cand_bytes = (size_t)count * AFFINITY_WORDS * sizeof(uint64_t);
        size_t dist_bytes = (size_t)count * sizeof(uint32_t);

        if (cudaMemcpy(d_aff_query,      query_host,      q_bytes,    cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        if (cudaMemcpy(d_aff_candidates,  candidates_host, cand_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;

        const int tpb = 256;
        int blocks = (int)((count + tpb - 1) / tpb);
        if (blocks < 1) blocks = 1;

        nearest_affinity_kernel<<<blocks, tpb>>>(d_aff_candidates, d_aff_query, d_aff_distances, count);

        if (cudaGetLastError()        != cudaSuccess) return -3;
        if (cudaDeviceSynchronize()   != cudaSuccess) return -4;
        if (cudaMemcpy(distances_host, d_aff_distances, dist_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;

        return 0;
    }

    int geometric_cuda(
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
            fprintf(stderr, "geometric_cuda: cudaMemcpy H->D failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -4;
        }

        const int threadsPerBlock = 256;
        int blocks = (int)((num_values + threadsPerBlock - 1) / threadsPerBlock);
        if (blocks < 1) blocks = 1;

        geometric_kernel<<<blocks, threadsPerBlock>>>((uint64_t*)d_pool_A, num_values);

        cudaError_t launchErr = cudaGetLastError();
        if (launchErr != cudaSuccess) {
            fprintf(stderr, "geometric_cuda: kernel launch failed: %s\n",
                    cudaGetErrorString(launchErr));
            return -2;
        }

        cudaError_t syncErr = cudaDeviceSynchronize();
        if (syncErr != cudaSuccess) {
            fprintf(stderr, "geometric_cuda: cudaDeviceSynchronize failed: %s\n",
                    cudaGetErrorString(syncErr));
            return -3;
        }

        cpyErr = cudaMemcpy(a_host, d_pool_A, bytes, cudaMemcpyDeviceToHost);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "geometric_cuda: cudaMemcpy D->H failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -6;
        }

        return 0;
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

        cudaError_t launchErr = cudaGetLastError();
        if (launchErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: kernel launch failed: %s\n",
                    cudaGetErrorString(launchErr));
            return -2;
        }
        cudaError_t syncErr = cudaDeviceSynchronize();
        if (syncErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaDeviceSynchronize failed: %s\n",
                    cudaGetErrorString(syncErr));
            return -3;
        }

        cpyErr = cudaMemcpy(a_host, d_pool_A, bytes, cudaMemcpyDeviceToHost);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "unified_bitwise_cuda: cudaMemcpy D->H failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -6;
        }
        return 0;
    }
}

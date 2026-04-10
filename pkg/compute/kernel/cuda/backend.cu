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

/*
Multivector is the Cl(3,0,1) 8-lane payload carried in the Value frame.
The frame remains uint64_t at rest so the Boolean and geometric ALUs share the
same 1024-byte ABI; CUDA reinterprets the lanes only at arithmetic boundaries.
*/
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

/*
NearestAffinity kernel — parallel Hamming distance search.

One thread per candidate. Each thread computes the Hamming distance
between the query vector and its candidate vector, writing the scalar
distance to an output buffer. The host reduces the argmin.
*/
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

/*
Geometric kernel — PGA lane for Value-local multivectors.

The opcode is read from the high nibble of Program[0], leaving the low nibble
available for Boolean truth tables. Context and Gradient are interpreted as
8x float64 multivectors and Signals receives the computed result.
*/
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

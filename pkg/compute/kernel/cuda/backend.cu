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

/*
hypercube_gossip_kernel diffuses each Value's S+C+G+P band across the
community using an XOR-routed hypercube. Parity: kernel.HypercubeGossipRef
(Metal) — missing neighbors use neutral 0, not self (XORing self^self=0
would corrupt the band). A full 256-Value field uses __shfl_xor_sync for
dimensions 0..4 (in-warp) and shared mem for 5..7, matching the Metal split.

A single block of value_count threads is launched per community.
*/
#define GOSSIP_BAND_START_WORD_CUDA   SIGNALS_START_WORD
#define GOSSIP_BAND_TARGET_WORD_CUDA  ASSET_START_WORD
#define GOSSIP_BAND_WORDS_CUDA        (SIGNALS_WORDS + CONTEXT_WORDS + GRADIENT_WORDS + PROPERTIES_WORDS)
#define GOSSIP_K_PER_CHUNK_CUDA       8
#define GOSSIP_FULL_HYPERCUBE_CUDA    256u

__device__ __forceinline__ uint64_t gossip_fold_ull(uint64_t a, uint64_t b, uint32_t fold_op) {
    return (fold_op == 0u) ? (a | b) : (a ^ b);
}

__device__ __forceinline__ uint64_t shfl_xor_ull(uint64_t v, int lane_mask) {
    unsigned int lo = (unsigned int)(v & 0xFFFFFFFFu);
    unsigned int hi = (unsigned int)(v >> 32);
    lo = __shfl_xor_sync(0xFFFFFFFFu, (int)lo, lane_mask, 32);
    hi = __shfl_xor_sync(0xFFFFFFFFu, (int)hi, lane_mask, 32);
    return (((uint64_t)hi) << 32) | (uint64_t)lo;
}

__global__ void hypercube_gossip_kernel(
    uint64_t*       arena,
    const uint32_t* indices,
    uint32_t        value_count,
    uint32_t        d_max,
    uint32_t        fold_op
) {
    extern __shared__ uint64_t shared[];

    uint32_t lid = threadIdx.x;
    bool active = lid < value_count;
    uint64_t* frame = nullptr;

    if (active) {
        frame = arena + (uint64_t)indices[lid] * (uint64_t)WORDS;

        for (uint32_t w = 0; w < GOSSIP_BAND_WORDS_CUDA; w++) {
            frame[GOSSIP_BAND_TARGET_WORD_CUDA + w] = frame[GOSSIP_BAND_START_WORD_CUDA + w];
        }
    }

    __syncthreads();

    for (uint32_t chunkBase = 0; chunkBase < GOSSIP_BAND_WORDS_CUDA; chunkBase += GOSSIP_K_PER_CHUNK_CUDA) {
        uint32_t chunkSize = GOSSIP_K_PER_CHUNK_CUDA;
        if (chunkBase + chunkSize > GOSSIP_BAND_WORDS_CUDA) {
            chunkSize = GOSSIP_BAND_WORDS_CUDA - chunkBase;
        }

        if (active && frame && value_count == GOSSIP_FULL_HYPERCUBE_CUDA) {
            uint64_t my_chunk[GOSSIP_K_PER_CHUNK_CUDA];
            for (uint32_t w = 0; w < chunkSize; w++) {
                my_chunk[w] = frame[GOSSIP_BAND_TARGET_WORD_CUDA + chunkBase + w];
            }
            for (uint32_t d = 0; d < d_max && d < 5u; d++) {
                int sh = (1 << (int)d);
                for (uint32_t w = 0; w < chunkSize; w++) {
                    uint64_t peer = shfl_xor_ull(my_chunk[w], sh);
                    my_chunk[w] = gossip_fold_ull(my_chunk[w], peer, fold_op);
                }
            }
            for (uint32_t d = 5; d < d_max; d++) {
                uint32_t dmask = 1u << d;
                for (uint32_t w = 0; w < chunkSize; w++) {
                    shared[(uint64_t)lid * GOSSIP_K_PER_CHUNK_CUDA + w] = my_chunk[w];
                }
                __syncthreads();
                for (uint32_t w = 0; w < chunkSize; w++) {
                    uint32_t nbr = lid ^ dmask;
                    uint64_t peer = shared[(uint64_t)nbr * GOSSIP_K_PER_CHUNK_CUDA + w];
                    my_chunk[w] = gossip_fold_ull(my_chunk[w], peer, fold_op);
                }
                __syncthreads();
            }
            for (uint32_t w = 0; w < chunkSize; w++) {
                frame[GOSSIP_BAND_TARGET_WORD_CUDA + chunkBase + w] = my_chunk[w];
            }
        } else {
            if (active) {
                for (uint32_t w = 0; w < chunkSize; w++) {
                    shared[(uint64_t)lid * GOSSIP_K_PER_CHUNK_CUDA + w] =
                        frame[GOSSIP_BAND_TARGET_WORD_CUDA + chunkBase + w];
                }
            }
            __syncthreads();

            for (uint32_t d = 0; d < d_max; d++) {
                uint32_t mask = 1u << d;
                uint64_t nbr_chunk[GOSSIP_K_PER_CHUNK_CUDA];

                if (active) {
                    uint32_t nbr = lid ^ mask;
                    bool nbr_valid = nbr < value_count;
                    for (uint32_t w = 0; w < chunkSize; w++) {
                        nbr_chunk[w] = nbr_valid
                            ? shared[(uint64_t)nbr * GOSSIP_K_PER_CHUNK_CUDA + w]
                            : 0uLL;
                    }
                }

                __syncthreads();

                if (active) {
                    for (uint32_t w = 0; w < chunkSize; w++) {
                        uint64_t self_w = shared[(uint64_t)lid * GOSSIP_K_PER_CHUNK_CUDA + w];
                        uint64_t fl = gossip_fold_ull(self_w, nbr_chunk[w], fold_op);
                        shared[(uint64_t)lid * GOSSIP_K_PER_CHUNK_CUDA + w] = fl;
                    }
                }

                __syncthreads();
            }

            if (active) {
                for (uint32_t w = 0; w < chunkSize; w++) {
                    frame[GOSSIP_BAND_TARGET_WORD_CUDA + chunkBase + w] =
                        shared[(uint64_t)lid * GOSSIP_K_PER_CHUNK_CUDA + w];
                }
            }
        }
        __syncthreads();
    }
}

// Padded affinity-row stride matches kernel/cpu/affinity_distances.go's
// affinityDistanceVectorWords (8 × uint64). The Go caller pre-packs the
// host buffer to this layout so all three backends (CPU, Metal, CUDA)
// consume identical memory.
#define AFFINITY_ROW_WORDS 8

__global__ void batch_first_fit_kernel(
    const uint64_t* community_ors,
    const uint64_t* value_affinities,
    int32_t*        out,
    uint32_t        community_count,
    uint32_t        value_count,
    uint32_t        hamming_budget,
    uint32_t        saturation_cap
) {
    uint32_t vid = blockIdx.x * blockDim.x + threadIdx.x;
    if (vid >= value_count) return;

    // Hold the value's 257 bits in registers across the entire community
    // sweep — never reload them inside the inner loop.
    uint32_t v_base = vid * AFFINITY_ROW_WORDS;
    uint64_t v0 = value_affinities[v_base + 0];
    uint64_t v1 = value_affinities[v_base + 1];
    uint64_t v2 = value_affinities[v_base + 2];
    uint64_t v3 = value_affinities[v_base + 3];
    uint64_t v4 = value_affinities[v_base + 4] & 1ULL;

    int32_t hit = -1;

    for (uint32_t c = 0; c < community_count; c++) {
        uint32_t c_base = c * AFFINITY_ROW_WORDS;
        uint64_t c0 = community_ors[c_base + 0];
        uint64_t c1 = community_ors[c_base + 1];
        uint64_t c2 = community_ors[c_base + 2];
        uint64_t c3 = community_ors[c_base + 3];
        uint64_t c4 = community_ors[c_base + 4] & 1ULL;

        uint32_t hamming =
            (uint32_t)__popcll(v0 ^ c0) +
            (uint32_t)__popcll(v1 ^ c1) +
            (uint32_t)__popcll(v2 ^ c2) +
            (uint32_t)__popcll(v3 ^ c3) +
            (uint32_t)(v4 ^ c4);

        if (hamming > hamming_budget) continue;

        uint32_t unionc =
            (uint32_t)__popcll(v0 | c0) +
            (uint32_t)__popcll(v1 | c1) +
            (uint32_t)__popcll(v2 | c2) +
            (uint32_t)__popcll(v3 | c3) +
            (uint32_t)(v4 | c4);

        if (unionc > saturation_cap) continue;

        hit = (int32_t)c;
        break;
    }

    out[vid] = hit;
}

__global__ void nearest_affinity_kernel(
    const uint64_t* candidates,
    const uint64_t* query,
    uint64_t*       best_packed_result,
    uint32_t        count
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= count) return;

    uint32_t base = id * AFFINITY_WORDS;
    uint32_t dist_sq = 0;

    for (int w = 0; w < AFFINITY_WORDS; w++) {
        dist_sq += __popcll(candidates[base + w] ^ query[w]);
    }

    uint32_t inverted_dist = 131072 - dist_sq;
    uint64_t packed_result = ((uint64_t)inverted_dist << 32) | (uint64_t)id;

    atomicMax((unsigned long long*)best_packed_result, (unsigned long long)packed_result);
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

// Device-side staging buffers for batch_first_fit. Sized in 8-word padded
// rows. Capacity grows monotonically and is freed by cleanup_cuda_pools.
static uint64_t* d_bff_communities = nullptr;
static uint64_t* d_bff_values      = nullptr;
static int32_t*  d_bff_out         = nullptr;
static uint32_t  bff_comm_cap      = 0;
static uint32_t  bff_val_cap       = 0;

// Pinned arena state for the gossip kernel.
// CUDA does not have Apple Silicon's unified shared memory; we allocate
// a device-resident arena and stream Value frames in/out per dispatch.
// gossip_arena_cap counts Value frames (not bytes) of headroom.
static uint64_t* d_gossip_arena   = nullptr;
static uint32_t* d_gossip_indices = nullptr;
static uint32_t  gossip_arena_cap = 0;
static uint32_t  gossip_idx_cap   = 0;

static int ensure_gossip_pool(uint32_t value_count) {
    if (value_count > gossip_arena_cap) {
        if (d_gossip_arena) { cudaFree(d_gossip_arena); d_gossip_arena = nullptr; }
        uint32_t cap = value_count * 2;
        if (cap < 256) cap = 256;
        size_t bytes = (size_t)cap * WORDS * sizeof(uint64_t);
        if (cudaMalloc((void**)&d_gossip_arena, bytes) != cudaSuccess) return -1;
        gossip_arena_cap = cap;
    }
    if (value_count > gossip_idx_cap) {
        if (d_gossip_indices) { cudaFree(d_gossip_indices); d_gossip_indices = nullptr; }
        uint32_t cap = value_count * 2;
        if (cap < 256) cap = 256;
        size_t bytes = (size_t)cap * sizeof(uint32_t);
        if (cudaMalloc((void**)&d_gossip_indices, bytes) != cudaSuccess) return -1;
        gossip_idx_cap = cap;
    }
    return 0;
}

static int ensure_bff_pool(uint32_t community_count, uint32_t value_count) {
    if (community_count > bff_comm_cap) {
        if (d_bff_communities) { cudaFree(d_bff_communities); d_bff_communities = nullptr; }
        uint32_t cap = community_count * 2;
        if (cap < 64) cap = 64;
        size_t bytes = (size_t)cap * AFFINITY_ROW_WORDS * sizeof(uint64_t);
        if (cudaMalloc((void**)&d_bff_communities, bytes) != cudaSuccess) return -1;
        bff_comm_cap = cap;
    }

    if (value_count > bff_val_cap) {
        if (d_bff_values) { cudaFree(d_bff_values); d_bff_values = nullptr; }
        if (d_bff_out)    { cudaFree(d_bff_out);    d_bff_out    = nullptr; }
        uint32_t cap = value_count * 2;
        if (cap < 256) cap = 256;
        size_t val_bytes = (size_t)cap * AFFINITY_ROW_WORDS * sizeof(uint64_t);
        size_t out_bytes = (size_t)cap * sizeof(int32_t);
        if (cudaMalloc((void**)&d_bff_values, val_bytes) != cudaSuccess) return -1;
        if (cudaMalloc((void**)&d_bff_out,    out_bytes) != cudaSuccess) return -1;
        bff_val_cap = cap;
    }

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
        if (d_bff_communities) { cudaFree(d_bff_communities); d_bff_communities = nullptr; }
        if (d_bff_values)      { cudaFree(d_bff_values);      d_bff_values      = nullptr; }
        if (d_bff_out)         { cudaFree(d_bff_out);         d_bff_out         = nullptr; }
        if (d_gossip_arena)    { cudaFree(d_gossip_arena);    d_gossip_arena    = nullptr; }
        if (d_gossip_indices)  { cudaFree(d_gossip_indices);  d_gossip_indices  = nullptr; }
        pool_capacity   = 0;
        aff_pool_cap    = 0;
        bff_comm_cap    = 0;
        bff_val_cap     = 0;
        gossip_arena_cap = 0;
        gossip_idx_cap   = 0;
    }

    /*
    hypercube_gossip_cuda runs the per-community gossip pipeline on
    CUDA. The host packs the resident community's Value frames
    contiguously starting at value_frames[0] and provides indices
    [0..value_count) corresponding to those packed slots. The kernel
    publishes the S+C+G+P band into asset and runs d_max XOR-routed
    fold steps with the chosen fold_op (0 = OR, 1 = XOR). After the
    kernel the host reads back the modified frames.
    */
    int hypercube_gossip_cuda(
        int       device_id,
        uint64_t* value_frames_host,
        uint32_t  value_count,
        uint32_t  d_max,
        uint32_t  fold_op
    ) {
        if (!value_frames_host || value_count == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_gossip_pool(value_count) != 0)    return -1;

        size_t frames_bytes  = (size_t)value_count * WORDS * sizeof(uint64_t);
        size_t indices_bytes = (size_t)value_count * sizeof(uint32_t);

        if (cudaMemcpy(d_gossip_arena, value_frames_host, frames_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;

        uint32_t* host_indices = (uint32_t*)malloc(indices_bytes);
        if (!host_indices) return -2;
        for (uint32_t i = 0; i < value_count; i++) host_indices[i] = i;

        cudaError_t cpy_idx = cudaMemcpy(d_gossip_indices, host_indices, indices_bytes, cudaMemcpyHostToDevice);
        free(host_indices);
        if (cpy_idx != cudaSuccess) return -2;

        size_t shared_bytes =
            (size_t)value_count * GOSSIP_K_PER_CHUNK_CUDA * sizeof(uint64_t);

        hypercube_gossip_kernel<<<1, value_count, shared_bytes>>>(
            d_gossip_arena, d_gossip_indices, value_count, d_max, fold_op
        );

        if (cudaGetLastError()      != cudaSuccess) return -3;
        if (cudaDeviceSynchronize() != cudaSuccess) return -4;
        if (cudaMemcpy(value_frames_host, d_gossip_arena, frames_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;

        return 0;
    }

    /*
    batch_first_fit_cuda runs the fused dual-gate first-fit routing
    kernel on the GPU. Inputs are 8-word padded rows (matching CPU and
    Metal). out_host receives one int32 per value: the first community
    index that satisfies the dual gate, or -1 when no community fits.
    */
    int batch_first_fit_cuda(
        int             device_id,
        const uint64_t* community_ors_host,
        uint32_t        community_count,
        const uint64_t* value_affinities_host,
        uint32_t        value_count,
        uint32_t        hamming_budget,
        uint32_t        saturation_cap,
        int32_t*        out_host
    ) {
        if (!out_host || value_count == 0) return -1;
        if (community_count > 0 && (!community_ors_host || !value_affinities_host)) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_bff_pool(community_count, value_count) != 0) return -1;

        size_t comm_bytes = (size_t)community_count * AFFINITY_ROW_WORDS * sizeof(uint64_t);
        size_t val_bytes  = (size_t)value_count     * AFFINITY_ROW_WORDS * sizeof(uint64_t);
        size_t out_bytes  = (size_t)value_count     * sizeof(int32_t);

        if (comm_bytes > 0) {
            if (cudaMemcpy(d_bff_communities, community_ors_host, comm_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        }
        if (cudaMemcpy(d_bff_values, value_affinities_host, val_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;

        const int tpb = 256;
        int blocks = (int)((value_count + tpb - 1) / tpb);
        if (blocks < 1) blocks = 1;

        batch_first_fit_kernel<<<blocks, tpb>>>(
            d_bff_communities,
            d_bff_values,
            d_bff_out,
            community_count,
            value_count,
            hamming_budget,
            saturation_cap
        );

        if (cudaGetLastError()      != cudaSuccess) return -3;
        if (cudaDeviceSynchronize() != cudaSuccess) return -4;
        if (cudaMemcpy(out_host, d_bff_out, out_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;

        return 0;
    }

    int nearest_affinity_cuda(
        int device_id,
        void* query_host,
        void* candidates_host,
        uint32_t count,
        uint64_t* best_packed_result_host
    ) {
        if (!query_host || !candidates_host || !best_packed_result_host || count == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_aff_pool(count) != 0) return -1;

        size_t q_bytes    = AFFINITY_WORDS * sizeof(uint64_t);
        size_t cand_bytes = (size_t)count * AFFINITY_WORDS * sizeof(uint64_t);
        size_t res_bytes  = sizeof(uint64_t);

        if (cudaMemcpy(d_aff_query,      query_host,      q_bytes,    cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        if (cudaMemcpy(d_aff_candidates,  candidates_host, cand_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;

        // Initialize best_packed_result to 0
        if (cudaMemset(d_aff_distances, 0, res_bytes) != cudaSuccess) return -2;

        const int tpb = 256;
        int blocks = (int)((count + tpb - 1) / tpb);
        if (blocks < 1) blocks = 1;

        nearest_affinity_kernel<<<blocks, tpb>>>(d_aff_candidates, d_aff_query, (uint64_t*)d_aff_distances, count);

        if (cudaGetLastError()        != cudaSuccess) return -3;
        if (cudaDeviceSynchronize()   != cudaSuccess) return -4;
        if (cudaMemcpy(best_packed_result_host, d_aff_distances, res_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;

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
}


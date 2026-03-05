#include "cuda.h"

#include <cuda_runtime.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct {
    uint64_t bits[8];
} Chord;

typedef struct {
    Chord blocks[27];
} MacroCube;

typedef struct {
    uint16_t header;
    uint8_t reserved[6];
    MacroCube cubes[5];
} __attribute__((aligned(8))) IcosahedralManifold;

__device__ static inline uint16_t manifold_winding(const IcosahedralManifold* m) {
    return (m->header >> 5) & 0xF;
}

__device__ static inline uint16_t manifold_rot_state(const IcosahedralManifold* m) {
    return (m->header >> 9) & 0x3F;
}

__device__ static inline uint16_t manifold_state(const IcosahedralManifold* m) {
    return (m->header >> 15) & 0x1;
}

__global__ void bitwise_best_fill_kernel(
    const IcosahedralManifold* dictionary,
    uint32_t num_chords,
    const IcosahedralManifold* active_context,
    const IcosahedralManifold* expected_reality,
    const uint8_t* geodesic_lut,
    unsigned long long* out_packed
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_chords) {
        return;
    }

    const IcosahedralManifold* candidate = &dictionary[id];

    uint16_t candidate_winding = manifold_winding(candidate);
    uint16_t context_winding = manifold_winding(active_context);
    if (candidate_winding != context_winding) {
        return;
    }

    uint16_t candidate_state = manifold_state(candidate);
    uint16_t context_state = manifold_state(active_context);
    if (candidate_state != context_state) {
        return;
    }

    uint32_t hamming_score = 0;
    uint32_t disorder_score = 0;
    uint32_t expectation_score = 0;

    #pragma unroll
    for (int c = 0; c < 5; c++) {
        #pragma unroll
        for (int b = 0; b < 27; b++) {
            #pragma unroll
            for (int i = 0; i < 8; i++) {
                uint64_t candidate_bits = candidate->cubes[c].blocks[b].bits[i];
                uint64_t context_bits = active_context->cubes[c].blocks[b].bits[i];
                uint64_t expected_bits = expected_reality->cubes[c].blocks[b].bits[i];

                hamming_score += __popcll(candidate_bits & context_bits);
                disorder_score += __popcll(candidate_bits & ~context_bits);
                expectation_score += __popcll(candidate_bits & expected_bits);
            }
        }
    }

    int score_fixed = (hamming_score * 500 + expectation_score * 1000 - disorder_score * 300) >> 10;

    uint16_t candidate_rot = manifold_rot_state(candidate);
    uint16_t context_rot = manifold_rot_state(active_context);
    uint16_t geodesic_dist = 255;
    if (geodesic_lut && context_rot < 60 && candidate_rot < 60) {
        geodesic_dist = geodesic_lut[context_rot * 60 + candidate_rot];
    }

    uint16_t inverted_dist = 65535 - geodesic_dist;

    unsigned long long packed =
        (((unsigned long long)(score_fixed & 0xFFFFFF)) << 40) |
        (((unsigned long long)inverted_dist) << 24) |
        (id & 0xFFFFFF);

    atomicMax(out_packed, packed);
}

int init_cuda(void) {
    int count = 0;
    cudaError_t err = cudaGetDeviceCount(&count);
    if (err != cudaSuccess || count <= 0) {
        return -1;
    }
    err = cudaSetDevice(0);
    if (err != cudaSuccess) {
        return -2;
    }
    return 0;
}

uint64_t bitwise_best_fill_cuda(
    const void* dictionary_ptr,
    uint32_t num_chords,
    const void* active_context_ptr,
    const void* expected_reality_ptr,
    const void* geodesic_lut_ptr
) {
    if (!dictionary_ptr || !active_context_ptr || num_chords == 0) {
        return 0;
    }

    const void* expected_ptr = expected_reality_ptr ? expected_reality_ptr : active_context_ptr;

    size_t dict_size = (size_t)num_chords * sizeof(IcosahedralManifold);
    size_t manifold_size = sizeof(IcosahedralManifold);
    size_t lut_size = 60 * 60;

    IcosahedralManifold* d_dict = NULL;
    IcosahedralManifold* d_context = NULL;
    IcosahedralManifold* d_expected = NULL;
    uint8_t* d_lut = NULL;
    unsigned long long* d_result = NULL;

    if (cudaMalloc((void**)&d_dict, dict_size) != cudaSuccess) {
        return 0;
    }
    if (cudaMalloc((void**)&d_context, manifold_size) != cudaSuccess) {
        cudaFree(d_dict);
        return 0;
    }
    if (cudaMalloc((void**)&d_expected, manifold_size) != cudaSuccess) {
        cudaFree(d_context);
        cudaFree(d_dict);
        return 0;
    }
    if (cudaMalloc((void**)&d_result, sizeof(unsigned long long)) != cudaSuccess) {
        cudaFree(d_expected);
        cudaFree(d_context);
        cudaFree(d_dict);
        return 0;
    }

    if (geodesic_lut_ptr) {
        if (cudaMalloc((void**)&d_lut, lut_size) != cudaSuccess) {
            cudaFree(d_result);
            cudaFree(d_expected);
            cudaFree(d_context);
            cudaFree(d_dict);
            return 0;
        }
    }

    unsigned long long zero = 0;
    cudaMemcpy(d_dict, dictionary_ptr, dict_size, cudaMemcpyHostToDevice);
    cudaMemcpy(d_context, active_context_ptr, manifold_size, cudaMemcpyHostToDevice);
    cudaMemcpy(d_expected, expected_ptr, manifold_size, cudaMemcpyHostToDevice);
    cudaMemcpy(d_result, &zero, sizeof(unsigned long long), cudaMemcpyHostToDevice);
    if (d_lut) {
        cudaMemcpy(d_lut, geodesic_lut_ptr, lut_size, cudaMemcpyHostToDevice);
    }

    int block_size = 256;
    int grid_size = (num_chords + block_size - 1) / block_size;

    bitwise_best_fill_kernel<<<grid_size, block_size>>>(
        d_dict,
        num_chords,
        d_context,
        d_expected,
        d_lut,
        d_result
    );

    uint64_t packed = 0;
    cudaMemcpy(&packed, d_result, sizeof(uint64_t), cudaMemcpyDeviceToHost);

    if (d_lut) {
        cudaFree(d_lut);
    }
    cudaFree(d_result);
    cudaFree(d_expected);
    cudaFree(d_context);
    cudaFree(d_dict);

    return packed;
}

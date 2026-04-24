#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

int count_metal_devices(void);
int init_metal(const char* metallib_path);

/*
init_metal_arena binds the host-pinned contiguous Value arena and the shared
linear allocation counter. Call once after primitive.EnsureArenaPinnedForGPU.
*/
int init_metal_arena(void* arena_base, size_t arena_bytes, uint32_t* linear_next_host);

/*
geometric_metal_indices runs the PGA (compose / sandwich / reverse) kernel on
indexed arena slots without staging copies. Kept alongside routing and gossip
GPU paths; the in-band VM is host-side via kernel.Optimizer.Run only.
*/
int geometric_metal_indices(const uint32_t* indices, uint32_t count);

int nearest_affinity_metal(void* query, void* candidates, uint32_t count, uint64_t* best_packed_result);

/*
batch_first_fit_metal runs the fused dual-gate first-fit routing kernel on
the GPU. Inputs match the CPU-side affinityDistanceVectorWords (8) padded
row layout. out_host receives one int32 per value: the index of the first
community that satisfies popcount(c XOR v) <= hamming_budget AND
popcount(c | v) <= saturation_cap, or -1 when no community fits.
*/
int batch_first_fit_metal(
    const uint64_t* community_ors_host,
    uint32_t        community_count,
    const uint64_t* value_affinities_host,
    uint32_t        value_count,
    uint32_t        hamming_budget,
    uint32_t        saturation_cap,
    int32_t*        out_host
);

/*
hypercube_gossip_metal_indices runs the hypercube gossip pass only. The host
runs kernel.RunUniversalBitwise and EMIT/TTL after this returns. indices are
value_count arena slot indices. fold_op: 0 = OR, 1 = XOR.
*/
int hypercube_gossip_metal_indices(
    const uint32_t* indices,
    uint32_t        value_count,
    uint32_t        d_max,
    uint32_t        fold_op
);

void cleanup_metal_pools(void);

#ifdef __cplusplus
}
#endif

#endif /* METAL_H */


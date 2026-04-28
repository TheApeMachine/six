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

/*
hypercube_gossip_metal_indices runs the resident packed AST over the indexed
community. indices preserves the caller's community positions; UINT32_MAX
marks nil Values. stage_indices receives community lanes selected by in-band
stage instructions and stage_count receives the number of valid entries.
*/
int hypercube_gossip_metal_indices(
    const uint32_t* indices,
    uint32_t        value_count,
    uint32_t        owner_index,
    uint32_t        owner_slot,
    uint32_t*       stage_indices,
    uint32_t*       stage_count
);

void cleanup_metal_pools(void);

#ifdef __cplusplus
}
#endif

#endif /* METAL_H */

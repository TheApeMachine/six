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
typedef struct {
    uint64_t kind;
    uint64_t start;
    uint64_t span;
    uint64_t threshold;
    uint64_t and_word;
    uint64_t threshold_b;
} predicate_device_spec_t;

int geometric_metal_indices(const uint32_t* indices, uint32_t count);

/*
hypercube_gossip_metal_indices runs the resident packed AST over the indexed
community. indices preserves the caller's community positions; UINT32_MAX
marks nil Values. spawn_* arrays are value_count entries and may contain
UINT32_MAX / zero for lanes that cannot emit.
*/
int hypercube_gossip_metal_indices(
    const uint32_t*                 indices,
    uint32_t                        value_count,
    uint32_t                        owner_index,
    uint32_t                        owner_slot,
    const predicate_device_spec_t*  predicates,
    const uint32_t*                 spawn_indices,
    const uint64_t*                 spawn_ids,
    uint8_t*                        spawn_active,
    uint32_t*                       stage_indices,
    uint32_t*                       stage_count
);

void cleanup_metal_pools(void);

#ifdef __cplusplus
}
#endif

#endif /* METAL_H */

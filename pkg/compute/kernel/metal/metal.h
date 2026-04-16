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
unified_bitwise_metal_indices dispatches unified_bitwise_arena_indices_kernel:
arena is the same base passed to init_metal_arena; indices lists arena slots.
max_slots is primitive.ArenaSlotCount.
*/
int unified_bitwise_metal_indices(const uint32_t* indices, uint32_t count, uint32_t max_slots);

/*
geometric_metal_indices runs the PGA kernel on indexed arena slots without
staging copies.
*/
int geometric_metal_indices(const uint32_t* indices, uint32_t count, uint32_t max_slots);

int nearest_affinity_metal(void* query, void* candidates, uint32_t count, uint32_t* distances);

void cleanup_metal_pools(void);

/*
metal_drain_spawn_queue copies up to max_out (parent, child) slot pairs written
by EMIT_CLONE. On success, out_count is the number of pairs copied into parents
and children; total_count (when non-NULL) receives the queue length n read from
bufSpawnTail before any removal. If n exceeds max_out, the tail is reduced by
max_out and the remaining pairs are compacted in the device buffers so events
are not discarded silently.
*/
int metal_drain_spawn_queue(
    uint32_t* parents,
    uint32_t* children,
    uint32_t max_out,
    uint32_t* out_count,
    uint32_t* total_count
);

#ifdef __cplusplus
}
#endif

#endif /* METAL_H */

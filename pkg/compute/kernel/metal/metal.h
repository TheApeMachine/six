#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// count_metal_devices returns the number of Metal-capable GPUs visible to the process.
// Uses MTLCopyAllDevices on macOS to enumerate all discrete and integrated GPUs.
int count_metal_devices(void);

// init_metal allocates default system devices and pipelines mapped directly from the path library.
// Returns 0 on success, or a non-zero error code upon failure preventing propagation.
int init_metal(const char* metallib_path);

/*
resolve_resonance_metal pushes asynchronous payload batches via GF(257) distance mapping.
Returns 0 on success.

Result encoding (out_result, uint64_t):
  - Upper 32 bits: inverted distance (maxEncodedDistSq - dist_squared). Higher = closer.
  - Lower 32 bits: node index of best match.

Example: packed=0x0001FFEA0000002b → idx=43, inverted_dist=131114 → distSq ≈ (134217728-131114)/1024 for CUDA scaled.
Valid inverted range [0, 131072] (CPU) or [0, 134217728] (CUDA scaled). Caller decodes via DecodePacked.
*/
int resolve_resonance_metal(const void* graph_nodes_ptr, uint32_t num_nodes, const void* active_context_ptr, uint64_t* out_result);

int resolve_phasedial_metal(const void* cache_nodes_ptr, uint32_t num_nodes, const void* query_dial_ptr, void* similarities_ptr);
int encode_phasedial_metal(const void* structural_phases_ptr, const void* primes_ptr, uint32_t num_values, void* out_dial_ptr);
int seq_toroidal_mean_phase_metal(const void* value_blocks_ptr, uint32_t num_values, double* out_theta, double* out_phi);
int weighted_circular_mean_metal(const void* value_blocks_ptr, uint32_t num_values, double* out_phase, double* out_concentration);
int solve_bvp_metal(const void* start_blocks_ptr, const void* goal_blocks_ptr, uint16_t* out_scale, uint16_t* out_translate, double* out_distance);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

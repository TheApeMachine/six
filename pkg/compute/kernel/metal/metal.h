#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// init_metal allocates default system devices and pipelines mapped directly from the path library.
// Returns 0 on success, or a non-zero error code upon failure preventing propagation.
int init_metal(const char* metallib_path);

/*
resolve_resonance_metal pushes asynchronous payload batches via GF(257) distance mapping.
Returns 0 on success.

Result encoding (out_result, uint64_t):
  - Upper 32 bits: inverted distance (maxEncodedDistSq - dist_squared). Higher = closer.
  - Lower 32 bits: node index of best match.

Example: packed=0x0002002a0000002b → idx=43, inverted_dist=858994731 → distSq ≈ 131072 - 858994731/1024.
Valid inverted range [0, 131072] (CPU) or [0, 134217728] (CUDA scaled). Caller decodes via DecodePacked.
*/
int resolve_resonance_metal(const void* graph_nodes_ptr, uint32_t num_nodes, const void* active_context_ptr, uint64_t* out_result);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

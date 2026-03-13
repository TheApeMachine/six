#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// init_metal allocates default system devices and pipelines mapped directly from the path library.
// Returns 0 on success, or a non-zero error code upon failure preventing propagation.
int init_metal(const char* metallib_path);

// resolve_resonance_metal pushes asynchronous payload batches via GF(257) distance mapping.
// Return 0 on success encoding bounded index into out_result. Values represent packed combinations where out_result holds priority on clean dispatches mapping to zero error values.
int resolve_resonance_metal(const void* graph_nodes_ptr, uint32_t num_nodes, const void* active_context_ptr, uint64_t* out_result);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

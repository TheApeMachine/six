#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

int init_metal(const char* metallib_path);
int resolve_resonance_metal(const void* graph_nodes_ptr, uint32_t num_nodes, const void* active_context_ptr, uint64_t* out_result);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

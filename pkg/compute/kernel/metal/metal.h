#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

int count_metal_devices(void);
int init_metal(const char* metallib_path);

/*
All dispatch functions operate on packed Value arrays.
Value layout: 128 contiguous uint64 words (1024 bytes) per Value.
num_values: number of Values in each array.
Returns 0 on success, negative on failure.
*/
int unified_bitwise_metal(void* a, uint32_t num_values);
int nearest_affinity_metal(void* query, void* candidates, uint32_t count, uint32_t* distances);

void cleanup_metal_pools(void);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

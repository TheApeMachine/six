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

int bitwise_or_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_and_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_xor_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_and_not_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_nand_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_nor_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_xnor_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_converse_nonimplication_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int bitwise_not_metal(const void* a, void* dst, uint32_t num_values);

int motor_apply_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int motor_invert_metal(const void* a, const void* b, void* dst, uint32_t num_values);
int motor_compose_metal(const void* a, const void* b, void* dst, uint32_t num_values);

int roll_left_metal(const void* src, void* dst, uint32_t shift, uint32_t num_values);

void cleanup_metal_pools(void);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

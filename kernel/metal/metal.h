#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

int init_metal(const char* metallib_path);
int bitwise_best_fill_metal(const void* dictionary_ptr, uint32_t num_chords, const void* active_context_ptr, const void* expected_reality_ptr, const void* expected_precision_ptr, const void* geodesic_lut_ptr, uint64_t* out_result);
int holographic_recall_metal(const void* substrate_filters_ptr, uint32_t num_filters, const void* prime_field_ptr, uint32_t target_rot_a, uint32_t target_rot_b, uint64_t* out_result);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

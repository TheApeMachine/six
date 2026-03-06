#ifndef METAL_H
#define METAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

int init_metal(const char* metallib_path);
uint64_t bitwise_best_fill_metal(const void* dictionary_ptr, uint32_t num_chords, const void* active_context_ptr, const void* expected_reality_ptr, const void* expected_precision_ptr, const void* geodesic_lut_ptr);

#ifdef __cplusplus
}
#endif

#endif // METAL_H

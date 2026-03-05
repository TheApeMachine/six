#ifndef SIX_CUDA_H
#define SIX_CUDA_H

#include <stdint.h>

int init_cuda(void);
uint64_t bitwise_best_fill_cuda(
    const void* dictionary_ptr,
    uint32_t num_chords,
    const void* active_context_ptr,
    const void* expected_reality_ptr,
    uint32_t target_id,
    const void* geodesic_lut_ptr
);

#endif

#include <metal_stdlib>
#include <metal_atomic>
using namespace metal;

// 512-bit Chord represented as 8x uint64
struct Chord {
    uint64_t bits[8];
};

struct MultiChord {
    Chord chords[5];
};

kernel void bitwise_best_fill(
    device const MultiChord* dictionary [[buffer(0)]],
    constant MultiChord* active_context [[buffer(1)]],
    device atomic_ulong* best_packed_result [[buffer(2)]],
    constant uint& num_chords [[buffer(3)]],
    constant uint& target_id [[buffer(4)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= num_chords) return;

    uint match_count = 0;
    uint noise_count = 0;

    MultiChord candidate = dictionary[id];
    MultiChord ctx = active_context[0];

#pragma unroll
    for (int p = 0; p < 5; p++) {
#pragma unroll
        for (int i = 0; i < 8; i++) {
            uint64_t c_bits = candidate.chords[p].bits[i];
            uint64_t a_bits = ctx.chords[p].bits[i];
            match_count += popcount(c_bits & a_bits);
            noise_count += popcount(c_bits & ~a_bits);
        }
    }

    float resonance = (float)match_count / (float)(match_count + noise_count + 1);

    // Score maps to 4,000,000 (~22 bits)
    const float SCORE_SCALE = 4000000.0f;
    uint score_fixed = (uint)(resonance * SCORE_SCALE);

    // Distance acts as tie-breaker (16 bits)
    uint distance = (uint)abs((int)id - (int)target_id);
    if (distance > 65535) {
        distance = 65535;
    }
    uint inverted_dist = 65535 - distance;

    // 24-bit limitation for id (up to 0xFFFFFF)
    uint safe_id = id;
    if (safe_id > 0xFFFFFF) {
        safe_id = 0xFFFFFF; // Guard against 24-bit truncation
    }

    // Pack: score (24-bit MSB) | inverted dist (16-bit) | raw_id (24-bit LSB)
    uint64_t packed_result = ((uint64_t)score_fixed << 40) | ((uint64_t)inverted_dist << 24) | (uint64_t)safe_id;

    // 1 instruction, 0 locks, 100% thread safe
    atomic_max_explicit(
        best_packed_result,
        (ulong)packed_result,
        memory_order_relaxed
    );
}
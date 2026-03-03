#include <metal_stdlib>
#include <metal_atomic>
using namespace metal;

// 512-bit Chord represented as 8x uint64
struct Chord {
    uint64_t bits[8];
};

struct BestFillResult {
    uint best_idx;
    float best_score;
};
// Compute Kernel for O(1) Hamming Distance Search against the entire LSM Tree / Substrate Dictionary
// This replaces the continuous Kuramoto dt solver with pure bitwise logic.
kernel void bitwise_best_fill(
    device const Chord* dictionary [[buffer(0)]], // The flat array of Bedrock Chords
    device const Chord* active_context [[buffer(1)]], // The Prompt/Context 512-bit state
    device atomic_ulong* best_packed_result [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint match_count = 0;
    uint noise_count = 0;

    // Load context and candidate chord to registers
    Chord candidate = dictionary[id];
    Chord ctx = active_context[0];

    // Compute popcounts across the 512 bits (8x uint64)
#pragma unroll
    for (int i = 0; i < 8; i++) {
        uint64_t c_bits = candidate.bits[i];
        uint64_t a_bits = ctx.bits[i];
        match_count += popcount(c_bits & a_bits);
        noise_count += popcount(c_bits & ~a_bits);
    }

    float resonance = (float)match_count / (float)(match_count + noise_count + 1);
    // Maps resonance to 32-bit range while avoiding overflow (scales to fill space better)
    const float SCORE_SCALE = 4294967.0f;
    uint score_fixed = (uint)(resonance * SCORE_SCALE);

    // Pack the score and the index into a single 64-bit unsigned int
    // Because the score is in the high 32 bits, atomic_max will natively sort by score first!
    uint64_t packed_result = ((uint64_t)score_fixed << 32) | (uint64_t)id;

    // 1 instruction, 0 locks, 100% thread safe
    atomic_max_explicit(
        best_packed_result, 
        (ulong)packed_result, 
        memory_order_relaxed
    );
}
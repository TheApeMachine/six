#include <metal_stdlib>
#include <metal_atomic>
using namespace metal;

// 512-bit Chord represented as 8x uint64
struct Chord {
    uint64_t bits[8];
};

/*
Compute Kernel: O(1) parallel bitwise containment search.

For each chord in the flat dictionary, checks how well it "fills"
the active context using popcount-based resonance scoring.
  - match_count = popcount(candidate & context)  — shared factors
  - noise_count = popcount(candidate & ~context) — extra noise

The winning index+score is packed into a single uint64 and reduced
via atomic_max (score in high 32 bits sorts naturally).
*/
kernel void bitwise_best_fill(
    device const Chord* dictionary [[buffer(0)]],
    constant Chord* active_context [[buffer(1)]],
    device atomic_ulong* best_packed_result [[buffer(2)]],
    constant uint& num_chords [[buffer(3)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= num_chords) return;

    uint match_count = 0;
    uint noise_count = 0;

    Chord candidate = dictionary[id];
    Chord ctx = active_context[0];

#pragma unroll
    for (int i = 0; i < 8; i++) {
        uint64_t c_bits = candidate.bits[i];
        uint64_t a_bits = ctx.bits[i];
        match_count += popcount(c_bits & a_bits);
        noise_count += popcount(c_bits & ~a_bits);
    }

    float resonance = (float)match_count / (float)(match_count + noise_count + 1);

    const float SCORE_SCALE = 4294967.0f;
    uint score_fixed = (uint)(resonance * SCORE_SCALE);

    // Pack score (high 32) | index (low 32). atomic_max sorts by score first.
    uint64_t packed_result = ((uint64_t)score_fixed << 32) | (uint64_t)id;

    // 1 instruction, 0 locks, 100% thread safe
    atomic_max_explicit(
        best_packed_result,
        (ulong)packed_result,
        memory_order_relaxed
    );
}
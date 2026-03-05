#include <metal_stdlib>
#include <metal_atomic>
using namespace metal;

// 512-bit Chord represented as 8x uint64
struct Chord {
    uint64_t bits[8];
};

struct MacroCube {
    Chord blocks[27];
};

struct ManifoldHeader {
    uint16_t data;
};

// V0.6 Primary Primitive (8.64KB SIMD Aligned)
struct IcosahedralManifold {
    ManifoldHeader header;
    uint8_t _padding[6];
    MacroCube cubes[5];
};

kernel void bitwise_best_fill(
    device const IcosahedralManifold* dictionary [[buffer(0)]],
    constant IcosahedralManifold* active_context [[buffer(1)]],
    device atomic_ulong* best_packed_result [[buffer(2)]],
    constant uint& num_chords [[buffer(3)]],
    constant uint& target_id [[buffer(4)]],
    constant IcosahedralManifold* expected_reality [[buffer(5)]],
    constant uint8_t* geodesic_lut [[buffer(6)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= num_chords) return;

    IcosahedralManifold candidate = dictionary[id];
    IcosahedralManifold ctx = active_context[0];
    // Expected reality acts as teleological anchor (can be ignored if null on host side)
    IcosahedralManifold expected = expected_reality[0];

    // PASS 1: Winding Filter (Integer)  [Winding is bits 5..8]
    uint16_t q_winding = (ctx.header.data >> 5) & 0x0F;
    uint16_t c_winding = (candidate.header.data >> 5) & 0x0F;
    if (q_winding != c_winding) return;

    // PASS 2: Group State Filter (Integer)  [State is bit 15]
    uint16_t q_state = (ctx.header.data >> 15) & 0x01;
    uint16_t c_state = (candidate.header.data >> 15) & 0x01;
    if (q_state != c_state) return;

    // RotState is bits 9..14
    uint16_t q_rot_state = (ctx.header.data >> 9) & 0x3F;
    uint16_t c_rot_state = (candidate.header.data >> 9) & 0x3F;

    // Determine sparse cube limits based on Mitosis state
    int max_cubes = (c_state == 1) ? 5 : 1;

    uint ctx_match_count = 0;
    uint ctx_noise_count = 0;
    uint exp_match_count = 0;
    uint exp_noise_count = 0;

    // PASS 3 & 4: Coarse SIMD & Dense Micro Popcount
#pragma unroll
    for (int c = 0; c < 5; c++) {
        // Warp bypass for structurally sparse 8.64KB primitives (Cubic Mode)
        if (c >= max_cubes) break; 
        
#pragma unroll
        for (int b = 0; b < 27; b++) {
#pragma unroll
            for (int i = 0; i < 8; i++) {
                uint64_t c_bits = candidate.cubes[c].blocks[b].bits[i];
                uint64_t a_bits = ctx.cubes[c].blocks[b].bits[i];
                uint64_t e_bits = expected.cubes[c].blocks[b].bits[i];

                ctx_match_count += popcount(c_bits & a_bits);
                ctx_noise_count += popcount(c_bits & ~a_bits);
                
                exp_match_count += popcount(c_bits & e_bits);
                exp_noise_count += popcount(c_bits & ~e_bits);
            }
        }
    }

    uint ctx_total = ctx_match_count + ctx_noise_count + 1;
    uint exp_total = exp_match_count + exp_noise_count + 1;
    
    // Scale purely on integer boundaries 
    uint SCORE_SCALE = 4000000;
    uint ctx_score = (ctx_match_count * SCORE_SCALE) / ctx_total;
    uint exp_score = (exp_match_count * SCORE_SCALE) / exp_total;
    
    // Teleological blending
    uint score_fixed = (ctx_score + exp_score) / 2;

    // PASS 5: O(1) Ambiguity Resolution (LUT)
    // Fetch true geodesic path distance between chiral states
    uint geodesic_dist = (uint)geodesic_lut[q_rot_state * 60 + c_rot_state];
    
    // Combine physical index location and shortest geodesic path as tie-breaker
    uint index_dist = (uint)abs((int)id - (int)target_id);
    if (index_dist > 65535) {
        index_dist = 65535;
    }
    
    // 16-bit tie breaker constraint: invert so highest bits represent lowest distance 
    uint inverted_dist = 65535 - ((index_dist + geodesic_dist) / 2);

    // Bounds checking for 24-bit LSB packing
    uint safe_id = id;
    if (safe_id > 0xFFFFFF) { safe_id = 0xFFFFFF; }

    // Pack: score (24-bit MSB) | inverted dist (16-bit) | raw_id (24-bit LSB)
    uint64_t packed_result = ((uint64_t)score_fixed << 40) | ((uint64_t)inverted_dist << 24) | (uint64_t)safe_id;

    // Lock-free maximum aggregate
    atomic_max_explicit(
        best_packed_result,
        (ulong)packed_result,
        memory_order_relaxed
    );
}
#include <metal_stdlib>
#include <metal_atomic>
using namespace metal;

struct GFRotation {
    uint16_t a;
    uint16_t b;
};

constant uint DISTANCE_MAX = 131072u;

// ctz64 returns the number of trailing zeros in x. Returns 64 if x == 0.
int ctz64(ulong x) {
    if (x == 0) return 64;
    int n = 0;
    if ((x & 0xFFFFFFFFUL) == 0) { n += 32; x >>= 32; }
    if ((x & 0xFFFFUL) == 0) { n += 16; x >>= 16; }
    if ((x & 0xFFUL) == 0) { n += 8; x >>= 8; }
    if ((x & 0xFUL) == 0) { n += 4; x >>= 4; }
    if ((x & 3UL) == 0) { n += 2; x >>= 2; }
    if ((x & 1UL) == 0) { n += 1; }
    return n;
}

kernel void resolve_resonance(
    device const GFRotation* graph_nodes [[buffer(0)]],
    constant GFRotation* active_context [[buffer(1)]],
    device atomic_ulong* best_packed_result [[buffer(2)]],
    constant uint& num_nodes [[buffer(3)]],
    constant uint& base_offset [[buffer(4)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= num_nodes) {
        return;
    }

    device const GFRotation& candidate = graph_nodes[id];
    constant GFRotation& ctx = active_context[0];

    int da = int(candidate.a) - int(ctx.a);
    int db = int(candidate.b) - int(ctx.b);
    uint dist_sq = uint(da * da + db * db);
    if (dist_sq > DISTANCE_MAX) {
        dist_sq = DISTANCE_MAX;
    }

    uint inverted_dist = DISTANCE_MAX - dist_sq;
    uint global_id = id + base_offset;
    uint64_t packed_result = ((uint64_t)inverted_dist << 32) | uint64_t(global_id);

    atomic_max_explicit(best_packed_result, ulong(packed_result), memory_order_relaxed);
}

// resolve_phasedial: one similarity per cache node. PhaseDial = 512 complex = 1024 floats.
kernel void resolve_phasedial(
    device const float* cache_nodes [[buffer(0)]],
    device const float* query_dial [[buffer(1)]],
    device float* similarities [[buffer(2)]],
    constant uint& num_nodes [[buffer(3)]],
    uint gid [[thread_position_in_grid]],
    uint tid [[thread_index_in_threadgroup]]
) {
    uint node_idx = gid / 32;
    uint lane = gid % 32;

    threadgroup float tg_query[1024];
    for (uint i = tid; i < 1024; i += 256) {
        tg_query[i] = query_dial[i];
    }
    threadgroup_barrier(mem_flags::mem_threadgroup);

    if (node_idx >= num_nodes) return;

    float dot = 0.0f;
    uint base = node_idx * 1024;
    for (uint i = lane; i < 1024; i += 32) {
        dot += cache_nodes[base + i] * tg_query[i];
    }
    for (uint offset = 16; offset > 0; offset /= 2) {
        dot += simd_shuffle_down(dot, offset);
    }
    if (lane == 0) {
        similarities[node_idx] = dot;
    }
}

// encode_phasedial: 512 output dimensions. structural_phases and primes are inputs.
kernel void encode_phasedial(
    device const float* structural_phases [[buffer(0)]],
    device const float* primes [[buffer(1)]],
    device float* out_dial [[buffer(2)]],
    constant uint& num_values [[buffer(3)]],
    uint k [[thread_position_in_grid]]
) {
    if (k >= 512) return;
    float omega = primes[k];
    float sum_real = 0.0f, sum_imag = 0.0f;
    for (uint t = 0; t < num_values; t++) {
        float phase = omega * float(t + 1) * 0.1f + structural_phases[t] * 2.0f * M_PI_F;
        float s = sin(phase), c = cos(phase);
        sum_real += c;
        sum_imag += s;
    }
    out_dial[k * 2] = sum_real;
    out_dial[k * 2 + 1] = sum_imag;
}

// seq_toroidal_mean_phase: per-value output [sin_theta, cos_theta, sin_phi, cos_phi]. Host reduces.
kernel void seq_toroidal_mean_phase(
    device const ulong* value_blocks [[buffer(0)]],
    device float* out_sums [[buffer(1)]],
    constant uint& num_values [[buffer(2)]],
    uint t [[thread_position_in_grid]]
) {
    if (t >= num_values) return;
    int active_count = 0;
    float v_sin = 0.0f, v_cos = 0.0f;
    uint base = t * 8;
    for (int blk = 0; blk < 8; blk++) {
        ulong block = value_blocks[base + blk];
        while (block != 0) {
            int bit_idx = ctz64(block);
            int prime_idx = blk * 64 + bit_idx;
            if (prime_idx < 257) {
                float angle = 2.0f * M_PI_F * float(prime_idx) / 257.0f;
                v_sin += sin(angle);
                v_cos += cos(angle);
                active_count++;
            }
            block &= block - 1UL;
        }
    }
    float v_theta = (v_sin != 0.0f || v_cos != 0.0f) ? atan2(v_sin, v_cos) : 0.0f;
    float v_phi = 2.0f * M_PI_F * float(active_count) / 257.0f;
    uint out_base = t * 4;
    out_sums[out_base + 0] = sin(v_theta);
    out_sums[out_base + 1] = cos(v_theta);
    out_sums[out_base + 2] = sin(v_phi);
    out_sums[out_base + 3] = cos(v_phi);
}

// weighted_circular_mean: per-value output [w*sin_theta, w*cos_theta, weight]. Host reduces.
kernel void weighted_circular_mean(
    device const ulong* value_blocks [[buffer(0)]],
    device float* out_sums [[buffer(1)]],
    constant uint& num_values [[buffer(2)]],
    uint t [[thread_position_in_grid]]
) {
    if (t >= num_values) return;
    int active_count = 0;
    float v_sin = 0.0f, v_cos = 0.0f;
    uint base = t * 8;
    for (int blk = 0; blk < 8; blk++) {
        ulong block = value_blocks[base + blk];
        while (block != 0) {
            int bit_idx = ctz64(block);
            int prime_idx = blk * 64 + bit_idx;
            if (prime_idx < 257) {
                float angle = 2.0f * M_PI_F * float(prime_idx) / 257.0f;
                v_sin += sin(angle);
                v_cos += cos(angle);
                active_count++;
            }
            block &= block - 1UL;
        }
    }
    float v_theta = (v_sin != 0.0f || v_cos != 0.0f) ? atan2(v_sin, v_cos) : 0.0f;
    float weight = (active_count > 0) ? float(active_count) : 1.0f;
    uint out_base = t * 3;
    out_sums[out_base + 0] = weight * sin(v_theta);
    out_sums[out_base + 1] = weight * cos(v_theta);
    out_sums[out_base + 2] = weight;
}

// solve_bvp: 65792 affine candidates. Output best via atomic_max.
kernel void solve_bvp(
    device const ulong* start_blocks [[buffer(0)]],
    device const ulong* goal_blocks [[buffer(1)]],
    device atomic_ulong* best_packed_result [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= 65792u) return;
    uint scale = (id / 257u) + 1u;
    uint translate = id % 257u;
    int match_count = 0;
    for (int blk = 0; blk < 8; blk++) {
        ulong s_block = start_blocks[blk];
        while (s_block != 0) {
            int bit_idx = ctz64(s_block);
            int prime_idx = blk * 64 + bit_idx;
            if (prime_idx < 257) {
                int p_prime = int((scale * uint(prime_idx) + translate) % 257u);
                int g_blk = p_prime / 64;
                int g_bit = p_prime % 64;
                if (g_blk < 8 && (goal_blocks[g_blk] & (1UL << g_bit)) != 0) {
                    match_count++;
                }
            }
            s_block &= s_block - 1UL;
        }
    }
    ulong packed = (ulong(match_count) << 32) | ulong(id);
    atomic_max_explicit(best_packed_result, packed, memory_order_relaxed);
}

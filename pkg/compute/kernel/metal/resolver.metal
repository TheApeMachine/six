#include <metal_stdlib>
#include <metal_atomic>
using namespace metal;

// GF(257) affine rotational state geometry
struct GFRotation {
    uint16_t a;
    uint16_t b;
};

// resolve_resonance finds the node in the substrate with the lowest
// geometric distance (da^2 + db^2) in GF(257) space.
kernel void resolve_resonance(
    device const GFRotation* graph_nodes [[buffer(0)]],
    constant GFRotation* active_context [[buffer(1)]],
    device atomic_ulong* best_packed_result [[buffer(2)]],
    constant uint& num_nodes [[buffer(3)]],
    constant uint& base_offset [[buffer(4)]],
    uint id [[thread_position_in_grid]]
) {
    if (id >= num_nodes) return;

    device const GFRotation& candidate = graph_nodes[id];
    constant GFRotation& ctx = active_context[0];

    // Node distance logic from the thermodynamic model:
    // da = abs(candidate.a - ctx.a) / 256.0
    // db = abs(candidate.b - ctx.b) / 256.0
    // d = sqrt(da*da + db*db)

    // Compute scaled distance
    float da = abs((float)candidate.a - (float)ctx.a); // 0 to 256
    float db = abs((float)candidate.b - (float)ctx.b); // 0 to 256
    float dist_sq = da*da + db*db; // range 0 to 131072

    // We want to find the MINIMUM distance.
    // atomic_max requires us to pack score such that MAX value is best.
    // So we invert the distance. Max dist_sq is 131072.
    // Let's pack: (131072 - dist_sq) in the upper 32 bits, and global_id in the lower 32.
    
    uint32_t dist_u32 = (uint32_t)dist_sq;
    uint32_t inverted_dist = 131072 - dist_u32; 

    uint global_id = id + base_offset;

    uint64_t packed_result = ((uint64_t)inverted_dist << 32) | (uint64_t)global_id;

    atomic_max_explicit(
        best_packed_result,
        (ulong)packed_result,
        memory_order_relaxed
    );
}

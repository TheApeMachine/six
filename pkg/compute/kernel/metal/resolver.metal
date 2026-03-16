#include <metal_stdlib>
#include <metal_atomic>
using namespace metal;

struct GFRotation {
    uint16_t a;
    uint16_t b;
};

constant uint DISTANCE_MAX = 131072u;

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

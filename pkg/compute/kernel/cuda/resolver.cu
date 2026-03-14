#include <cuda_runtime.h>
#include <stdint.h>
#include <math.h>

extern "C" {

struct GFRotation {
    uint16_t a;
    uint16_t b;
};

__global__ void resolve_resonance_kernel(
    const GFRotation* graph_nodes,
    const GFRotation* active_context,
    unsigned long long* best_packed_result,
    uint32_t num_nodes,
    uint32_t base_offset
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_nodes) return;

    GFRotation candidate = graph_nodes[id];
    GFRotation ctx = active_context[0];

    // Node distance logic matching thermodynamic model:
    float da = fabsf((float)candidate.a - (float)ctx.a);
    float db = fabsf((float)candidate.b - (float)ctx.b);
    float dist_sq = da*da + db*db;

    // Scale before cast to preserve fractional distance; clamp against scaled max
    const float SCALE = 1024.0f;
    const uint32_t SCALED_MAX = 131072u * (uint32_t)SCALE;
    float dist_scaled = dist_sq * SCALE;
    uint32_t dist_u32 = (uint32_t)dist_scaled;
    if (dist_u32 > SCALED_MAX) {
        dist_u32 = SCALED_MAX;
    }
    uint32_t inverted_dist = SCALED_MAX - dist_u32;

    uint32_t global_id = id + base_offset;

    unsigned long long packed_result = ((unsigned long long)inverted_dist << 32) | (unsigned long long)global_id;

    atomicMax(best_packed_result, packed_result);
}

int cuda_device_count() {
    int count = 0;
    if (cudaGetDeviceCount(&count) != cudaSuccess) {
        return 0;
    }
    return count;
}

int resolve_resonance_cuda(
    int device_id,
    const void* graph_nodes_ptr,
    uint32_t num_nodes,
    const void* active_context_ptr,
    uint64_t* out_result
) {
    if (num_nodes == 0) {
        *out_result = 0;
        return 0;
    }

    cudaError_t err = cudaSetDevice(device_id);
    if (err != cudaSuccess) return -1;

    GFRotation* d_active_context = nullptr;
    unsigned long long* d_best_packed_result = nullptr;
    err = cudaMalloc((void**)&d_active_context, sizeof(GFRotation));
    if (err != cudaSuccess) return -1;

    err = cudaMalloc((void**)&d_best_packed_result, sizeof(unsigned long long));
    if (err != cudaSuccess) {
        cudaFree(d_active_context);
        return -1;
    }

    GFRotation* d_graph_nodes = nullptr;
    err = cudaMalloc((void**)&d_graph_nodes, num_nodes * sizeof(GFRotation));
    if (err != cudaSuccess) return -1;

    unsigned long long initial_val = 0;
    
    err = cudaMemcpy(d_graph_nodes, graph_nodes_ptr, num_nodes * sizeof(GFRotation), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) {
        cudaFree(d_graph_nodes);
        cudaFree(d_active_context);
        cudaFree(d_best_packed_result);
        return -1;
    }

    err = cudaMemcpy(d_active_context, active_context_ptr, sizeof(GFRotation), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) {
        cudaFree(d_graph_nodes);
        cudaFree(d_active_context);
        cudaFree(d_best_packed_result);
        return -1;
    }

    err = cudaMemcpy(d_best_packed_result, &initial_val, sizeof(unsigned long long), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) {
        cudaFree(d_graph_nodes);
        cudaFree(d_active_context);
        cudaFree(d_best_packed_result);
        return -1;
    }

    uint32_t threadsPerBlock = 256;
    uint32_t blocksPerGrid = (num_nodes + threadsPerBlock - 1) / threadsPerBlock;

    resolve_resonance_kernel<<<blocksPerGrid, threadsPerBlock>>>(
        d_graph_nodes,
        d_active_context,
        d_best_packed_result,
        num_nodes,
        0
    );

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) {
        cudaFree(d_graph_nodes);
        return -2;
    }

    err = cudaMemcpy(out_result, d_best_packed_result, sizeof(unsigned long long), cudaMemcpyDeviceToHost);

    cudaFree(d_graph_nodes);
    cudaFree(d_active_context);
    cudaFree(d_best_packed_result);

    if (err != cudaSuccess) {
        return -3;
    }

    return 0;
}

} // extern "C"

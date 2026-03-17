#include <cuda_runtime.h>
#include <stdint.h>

extern "C" {

struct GFRotation {
    uint16_t a;
    uint16_t b;
};

static const uint32_t DISTANCE_MAX = 131072u;
static const uint32_t SCALED_DISTANCE_MAX = DISTANCE_MAX;

__global__ void resolve_resonance_kernel(
    const GFRotation* graph_nodes,
    const GFRotation* active_context,
    unsigned long long* best_packed_result,
    uint32_t num_nodes,
    uint32_t base_offset
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_nodes) {
        return;
    }

    GFRotation candidate = graph_nodes[id];
    GFRotation ctx = active_context[0];

    uint32_t da = (uint32_t)candidate.a - (uint32_t)ctx.a;
    uint32_t db = (uint32_t)candidate.b - (uint32_t)ctx.b;
    uint64_t dist_sq64 = (uint64_t)da * da + (uint64_t)db * db;
    if (dist_sq64 > DISTANCE_MAX) {
        dist_sq64 = DISTANCE_MAX;
    }

    uint32_t dist_sq = (uint32_t)dist_sq64;
    uint32_t inverted_dist = SCALED_DISTANCE_MAX - dist_sq;
    uint32_t global_id = id + base_offset;

    unsigned long long packed_result =
        ((unsigned long long)inverted_dist << 32) | (unsigned long long)global_id;

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
    if (out_result == nullptr) {
        return -1;
    }
    if (num_nodes == 0) {
        *out_result = 0;
        return 0;
    }

    int status_code = 0;
    cudaError_t err = cudaSetDevice(device_id);
    if (err != cudaSuccess) {
        return -1;
    }

    GFRotation* d_graph_nodes = nullptr;
    GFRotation* d_active_context = nullptr;
    unsigned long long* d_best_packed_result = nullptr;
    unsigned long long initial_val = 0ULL;

    err = cudaMalloc((void**)&d_graph_nodes, num_nodes * sizeof(GFRotation));
    if (err != cudaSuccess) {
        status_code = -1;
        goto cleanup;
    }

    err = cudaMalloc((void**)&d_active_context, sizeof(GFRotation));
    if (err != cudaSuccess) {
        status_code = -1;
        goto cleanup;
    }

    err = cudaMalloc((void**)&d_best_packed_result, sizeof(unsigned long long));
    if (err != cudaSuccess) {
        status_code = -1;
        goto cleanup;
    }

    err = cudaMemcpy(d_graph_nodes, graph_nodes_ptr, num_nodes * sizeof(GFRotation), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) {
        status_code = -1;
        goto cleanup;
    }

    err = cudaMemcpy(d_active_context, active_context_ptr, sizeof(GFRotation), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) {
        status_code = -1;
        goto cleanup;
    }

    err = cudaMemcpy(d_best_packed_result, &initial_val, sizeof(unsigned long long), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) {
        status_code = -1;
        goto cleanup;
    }

    uint32_t threads_per_block = 256u;
    uint32_t blocks_per_grid = (num_nodes + threads_per_block - 1u) / threads_per_block;

    resolve_resonance_kernel<<<blocks_per_grid, threads_per_block>>>(
        d_graph_nodes,
        d_active_context,
        d_best_packed_result,
        num_nodes,
        0u
    );

    err = cudaGetLastError();
    if (err != cudaSuccess) {
        status_code = -2;
        goto cleanup;
    }

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) {
        status_code = -2;
        goto cleanup;
    }

    err = cudaMemcpy(out_result, d_best_packed_result, sizeof(unsigned long long), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) {
        status_code = -3;
        goto cleanup;
    }

cleanup:
    if (d_graph_nodes != nullptr) {
        cudaFree(d_graph_nodes);
    }
    if (d_active_context != nullptr) {
        cudaFree(d_active_context);
    }
    if (d_best_packed_result != nullptr) {
        cudaFree(d_best_packed_result);
    }

    return status_code;
}

} // extern "C"

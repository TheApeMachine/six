#include <cuda_runtime.h>
#include <stdint.h>
#include <math.h>

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

__global__ void resolve_phasedial_kernel(
    const double* cache_nodes,
    const double* query_dial,
    double* similarities,
    uint32_t num_nodes
) {
    uint32_t node_idx = blockIdx.x * (blockDim.x / 32) + (threadIdx.x / 32);
    if (node_idx >= num_nodes) return;
    uint32_t lane_idx = threadIdx.x % 32;

    __shared__ double s_query[1024];
    for (int i = threadIdx.x; i < 1024; i += blockDim.x) {
        s_query[i] = query_dial[i];
    }
    __syncthreads();

    double dot = 0.0;
    int base_idx = node_idx * 1024;
    for (int i = lane_idx; i < 1024; i += 32) {
        dot += cache_nodes[base_idx + i] * s_query[i];
    }
    for (int offset = 16; offset > 0; offset /= 2) {
        dot += __shfl_down_sync(0xffffffff, dot, offset);
    }
    if (lane_idx == 0) {
        similarities[node_idx] = dot;
    }
}

__global__ void encode_phasedial_kernel(
    const double* structural_phases,
    const float* primes,
    double* out_dial,
    uint32_t num_values
) {
    uint32_t k = blockIdx.x * blockDim.x + threadIdx.x;
    if (k >= 512) return;
    double omega = (double)primes[k];
    double sum_real = 0.0;
    double sum_imag = 0.0;
    for (uint32_t t = 0; t < num_values; t++) {
        double phase = (omega * (double)(t + 1) * 0.1) + (structural_phases[t] * 2.0 * M_PI);
        double s, c;
        sincos(phase, &s, &c);
        sum_real += c;
        sum_imag += s;
    }
    out_dial[k * 2] = sum_real;
    out_dial[k * 2 + 1] = sum_imag;
}

__global__ void seq_toroidal_mean_phase_kernel(
    const uint64_t* value_blocks,
    double* out_sums,
    uint32_t num_values
) {
    uint32_t t = blockIdx.x * blockDim.x + threadIdx.x;
    if (t >= num_values) return;
    int active_count = 0;
    double v_sin = 0.0, v_cos = 0.0;
    int base_idx = t * 8;
    for (int blk = 0; blk < 8; blk++) {
        uint64_t block = value_blocks[base_idx + blk];
        while (block != 0) {
            int bit_idx = __ffsll(block) - 1;
            int prime_idx = blk * 64 + bit_idx;
            if (prime_idx < 257) {
                double angle = 2.0 * M_PI * (double)prime_idx / 257.0;
                double s, c;
                sincos(angle, &s, &c);
                v_sin += s;
                v_cos += c;
                active_count++;
            }
            block &= block - 1;
        }
    }
    double v_theta = (v_sin != 0.0 || v_cos != 0.0) ? atan2(v_sin, v_cos) : 0.0;
    double v_phi = 2.0 * M_PI * (double)active_count / 257.0;
    double s_t, c_t, s_p, c_p;
    sincos(v_theta, &s_t, &c_t);
    sincos(v_phi, &s_p, &c_p);
    atomicAdd(&out_sums[0], s_t);
    atomicAdd(&out_sums[1], c_t);
    atomicAdd(&out_sums[2], s_p);
    atomicAdd(&out_sums[3], c_p);
}

__global__ void weighted_circular_mean_kernel(
    const uint64_t* value_blocks,
    double* out_sums,
    uint32_t num_values
) {
    uint32_t t = blockIdx.x * blockDim.x + threadIdx.x;
    if (t >= num_values) return;
    int active_count = 0;
    double v_sin = 0.0, v_cos = 0.0;
    int base_idx = t * 8;
    for (int blk = 0; blk < 8; blk++) {
        uint64_t block = value_blocks[base_idx + blk];
        while (block != 0) {
            int bit_idx = __ffsll(block) - 1;
            int prime_idx = blk * 64 + bit_idx;
            if (prime_idx < 257) {
                double angle = 2.0 * M_PI * (double)prime_idx / 257.0;
                double s, c;
                sincos(angle, &s, &c);
                v_sin += s;
                v_cos += c;
                active_count++;
            }
            block &= block - 1;
        }
    }
    double v_theta = (v_sin != 0.0 || v_cos != 0.0) ? atan2(v_sin, v_cos) : 0.0;
    double weight = (double)(active_count > 0 ? active_count : 1);
    double s_t, c_t;
    sincos(v_theta, &s_t, &c_t);
    atomicAdd(&out_sums[0], weight * s_t);
    atomicAdd(&out_sums[1], weight * c_t);
    atomicAdd(&out_sums[2], weight);
}

__global__ void solve_bvp_kernel(
    const uint64_t* start_blocks,
    const uint64_t* goal_blocks,
    unsigned long long* best_packed_result
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= 65792) return;
    uint32_t scale = (id / 257) + 1;
    uint32_t translate = id % 257;
    int match_count = 0;
    for (int blk = 0; blk < 8; blk++) {
        uint64_t s_block = start_blocks[blk];
        while (s_block != 0) {
            int bit_idx = __ffsll(s_block) - 1;
            int prime_idx = blk * 64 + bit_idx;
            if (prime_idx < 257) {
                int p_prime = (scale * prime_idx + translate) % 257;
                int g_blk = p_prime / 64;
                int g_bit = p_prime % 64;
                if (g_blk < 8 && (goal_blocks[g_blk] & (1ULL << g_bit))) {
                    match_count++;
                }
            }
            s_block &= s_block - 1;
        }
    }
    unsigned long long packed = ((unsigned long long)match_count << 32) | (unsigned long long)id;
    atomicMax(best_packed_result, packed);
}

int resolve_phasedial_cuda(
    int device_id,
    const void* cache_nodes_ptr,
    uint32_t num_nodes,
    const void* query_dial_ptr,
    void* similarities_ptr
) {
    if (cache_nodes_ptr == nullptr || query_dial_ptr == nullptr || similarities_ptr == nullptr || num_nodes == 0) {
        return -1;
    }
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;

    double* d_cache = nullptr;
    double* d_query = nullptr;
    double* d_sims = nullptr;
    size_t cache_bytes = (size_t)num_nodes * 1024 * sizeof(double);

    cudaError_t err = cudaMalloc((void**)&d_cache, cache_bytes);
    if (err != cudaSuccess) return -1;
    err = cudaMalloc((void**)&d_query, 1024 * sizeof(double));
    if (err != cudaSuccess) { cudaFree(d_cache); return -1; }
    err = cudaMalloc((void**)&d_sims, num_nodes * sizeof(double));
    if (err != cudaSuccess) { cudaFree(d_cache); cudaFree(d_query); return -1; }

    err = cudaMemcpy(d_cache, cache_nodes_ptr, cache_bytes, cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_phasedial;
    err = cudaMemcpy(d_query, query_dial_ptr, 1024 * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_phasedial;

    resolve_phasedial_kernel<<<(num_nodes + 7) / 8, 256>>>(d_cache, d_query, d_sims, num_nodes);
    err = cudaGetLastError();
    if (err != cudaSuccess) goto cleanup_phasedial;
    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) goto cleanup_phasedial;

    err = cudaMemcpy(similarities_ptr, d_sims, num_nodes * sizeof(double), cudaMemcpyDeviceToHost);

cleanup_phasedial:
    cudaFree(d_cache);
    cudaFree(d_query);
    cudaFree(d_sims);
    return (err != cudaSuccess) ? -1 : 0;
}

int encode_phasedial_cuda(
    int device_id,
    const void* structural_phases_ptr,
    const void* primes_ptr,
    uint32_t num_values,
    void* out_dial_ptr
) {
    if (structural_phases_ptr == nullptr || primes_ptr == nullptr || out_dial_ptr == nullptr) {
        return -1;
    }
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;

    double* d_phases = nullptr;
    float* d_primes = nullptr;
    double* d_out = nullptr;

    cudaError_t err = cudaMalloc((void**)&d_phases, num_values * sizeof(double));
    if (err != cudaSuccess) return -1;
    err = cudaMalloc((void**)&d_primes, 512 * sizeof(float));
    if (err != cudaSuccess) { cudaFree(d_phases); return -1; }
    err = cudaMalloc((void**)&d_out, 1024 * sizeof(double));
    if (err != cudaSuccess) { cudaFree(d_phases); cudaFree(d_primes); return -1; }

    err = cudaMemcpy(d_phases, structural_phases_ptr, num_values * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_encode;
    err = cudaMemcpy(d_primes, primes_ptr, 512 * sizeof(float), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_encode;

    encode_phasedial_kernel<<<(512 + 255) / 256, 256>>>(d_phases, d_primes, d_out, num_values);
    err = cudaGetLastError();
    if (err != cudaSuccess) goto cleanup_encode;
    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) goto cleanup_encode;

    err = cudaMemcpy(out_dial_ptr, d_out, 1024 * sizeof(double), cudaMemcpyDeviceToHost);

cleanup_encode:
    cudaFree(d_phases);
    cudaFree(d_primes);
    cudaFree(d_out);
    return (err != cudaSuccess) ? -1 : 0;
}

int seq_toroidal_mean_phase_cuda(
    int device_id,
    const void* value_blocks_ptr,
    uint32_t num_values,
    double* out_theta,
    double* out_phi
) {
    if (value_blocks_ptr == nullptr || out_theta == nullptr || out_phi == nullptr || num_values == 0) {
        return -1;
    }
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;

    uint64_t* d_blocks = nullptr;
    double* d_sums = nullptr;

    cudaError_t err = cudaMalloc((void**)&d_blocks, num_values * 8 * sizeof(uint64_t));
    if (err != cudaSuccess) return -1;
    err = cudaMalloc((void**)&d_sums, 4 * sizeof(double));
    if (err != cudaSuccess) { cudaFree(d_blocks); return -1; }

    double zeros[4] = {0.0, 0.0, 0.0, 0.0};
    err = cudaMemcpy(d_blocks, value_blocks_ptr, num_values * 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_seq;
    err = cudaMemcpy(d_sums, zeros, 4 * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_seq;

    seq_toroidal_mean_phase_kernel<<<(num_values + 255) / 256, 256>>>(d_blocks, d_sums, num_values);
    err = cudaGetLastError();
    if (err != cudaSuccess) goto cleanup_seq;
    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) goto cleanup_seq;

    double sums[4];
    err = cudaMemcpy(sums, d_sums, 4 * sizeof(double), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) goto cleanup_seq;

    *out_theta = atan2(sums[0], sums[1]);
    *out_phi = atan2(sums[2], sums[3]);
    if (*out_phi < 0.0) *out_phi += 2.0 * M_PI;

cleanup_seq:
    cudaFree(d_blocks);
    cudaFree(d_sums);
    return (err != cudaSuccess) ? -1 : 0;
}

int weighted_circular_mean_cuda(
    int device_id,
    const void* value_blocks_ptr,
    uint32_t num_values,
    double* out_phase,
    double* out_concentration
) {
    if (value_blocks_ptr == nullptr || out_phase == nullptr || out_concentration == nullptr || num_values == 0) {
        return -1;
    }
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;

    uint64_t* d_blocks = nullptr;
    double* d_sums = nullptr;

    cudaError_t err = cudaMalloc((void**)&d_blocks, num_values * 8 * sizeof(uint64_t));
    if (err != cudaSuccess) return -1;
    err = cudaMalloc((void**)&d_sums, 3 * sizeof(double));
    if (err != cudaSuccess) { cudaFree(d_blocks); return -1; }

    double zeros[3] = {0.0, 0.0, 0.0};
    err = cudaMemcpy(d_blocks, value_blocks_ptr, num_values * 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_wcm;
    err = cudaMemcpy(d_sums, zeros, 3 * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_wcm;

    weighted_circular_mean_kernel<<<(num_values + 255) / 256, 256>>>(d_blocks, d_sums, num_values);
    err = cudaGetLastError();
    if (err != cudaSuccess) goto cleanup_wcm;
    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) goto cleanup_wcm;

    double sums[3];
    err = cudaMemcpy(sums, d_sums, 3 * sizeof(double), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) goto cleanup_wcm;

    *out_phase = atan2(sums[0], sums[1]);
    if (*out_phase < 0.0) *out_phase += 2.0 * M_PI;
    double r = sqrt(sums[0] * sums[0] + sums[1] * sums[1]);
    *out_concentration = (sums[2] > 0.0) ? (r / sums[2]) : 0.0;

cleanup_wcm:
    cudaFree(d_blocks);
    cudaFree(d_sums);
    return (err != cudaSuccess) ? -1 : 0;
}

int solve_bvp_cuda(
    int device_id,
    const void* start_blocks_ptr,
    const void* goal_blocks_ptr,
    uint16_t* out_scale,
    uint16_t* out_translate,
    double* out_distance
) {
    if (start_blocks_ptr == nullptr || goal_blocks_ptr == nullptr ||
        out_scale == nullptr || out_translate == nullptr || out_distance == nullptr) {
        return -1;
    }
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;

    uint64_t* d_start = nullptr;
    uint64_t* d_goal = nullptr;
    unsigned long long* d_best = nullptr;

    cudaError_t err = cudaMalloc((void**)&d_start, 8 * sizeof(uint64_t));
    if (err != cudaSuccess) return -1;
    err = cudaMalloc((void**)&d_goal, 8 * sizeof(uint64_t));
    if (err != cudaSuccess) { cudaFree(d_start); return -1; }
    err = cudaMalloc((void**)&d_best, sizeof(unsigned long long));
    if (err != cudaSuccess) { cudaFree(d_start); cudaFree(d_goal); return -1; }

    unsigned long long init = 0ULL;
    err = cudaMemcpy(d_start, start_blocks_ptr, 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_bvp;
    err = cudaMemcpy(d_goal, goal_blocks_ptr, 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_bvp;
    err = cudaMemcpy(d_best, &init, sizeof(unsigned long long), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) goto cleanup_bvp;

    solve_bvp_kernel<<<(65792 + 255) / 256, 256>>>(d_start, d_goal, d_best);
    err = cudaGetLastError();
    if (err != cudaSuccess) goto cleanup_bvp;
    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) goto cleanup_bvp;

    unsigned long long packed;
    err = cudaMemcpy(&packed, d_best, sizeof(unsigned long long), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) goto cleanup_bvp;

    uint32_t id = (uint32_t)(packed & 0xFFFFFFFFULL);
    uint32_t match_count = (uint32_t)(packed >> 32);
    *out_scale = (uint16_t)((id / 257) + 1);
    *out_translate = (uint16_t)(id % 257);
    *out_distance = 1.0 - ((double)match_count / (double)257.0);

cleanup_bvp:
    cudaFree(d_start);
    cudaFree(d_goal);
    cudaFree(d_best);
    return (err != cudaSuccess) ? -1 : 0;
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

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

/* ─── Resident VRAM buffer pools ──────────────────────────────────────── */

static GFRotation* d_graph_pool = nullptr;
static GFRotation* d_ctx_pool = nullptr;
static unsigned long long* d_result_pool = nullptr;
static uint32_t d_graph_pool_capacity = 0;

static double* d_phasedial_cache = nullptr;
static double* d_phasedial_query = nullptr;
static double* d_phasedial_sims = nullptr;
static uint32_t d_phasedial_capacity = 0;

static double* d_encode_phases = nullptr;
static float* d_encode_primes = nullptr;
static double* d_encode_out = nullptr;
static uint32_t d_encode_capacity = 0;

static uint64_t* d_blocks_pool = nullptr;
static double* d_sums_pool = nullptr;
static uint32_t d_blocks_capacity = 0;

static uint64_t* d_bvp_start = nullptr;
static uint64_t* d_bvp_goal = nullptr;
static unsigned long long* d_bvp_best = nullptr;
static int d_bvp_allocated = 0;

/* ─── Pool growth functions ───────────────────────────────────────────── */

static int ensure_resonance_pool(uint32_t num_nodes) {
    if (d_graph_pool != nullptr && d_graph_pool_capacity >= num_nodes) {
        return 0;
    }

    if (d_graph_pool) cudaFree(d_graph_pool);
    if (d_ctx_pool) cudaFree(d_ctx_pool);
    if (d_result_pool) cudaFree(d_result_pool);

    uint32_t capacity = num_nodes * 2;
    if (capacity < 1024) capacity = 1024;

    cudaError_t err = cudaMalloc((void**)&d_graph_pool, capacity * sizeof(GFRotation));
    if (err != cudaSuccess) { d_graph_pool = nullptr; d_graph_pool_capacity = 0; return -1; }

    err = cudaMalloc((void**)&d_ctx_pool, sizeof(GFRotation));
    if (err != cudaSuccess) {
        cudaFree(d_graph_pool); d_graph_pool = nullptr; d_graph_pool_capacity = 0; return -1;
    }

    err = cudaMalloc((void**)&d_result_pool, sizeof(unsigned long long));
    if (err != cudaSuccess) {
        cudaFree(d_graph_pool); cudaFree(d_ctx_pool);
        d_graph_pool = nullptr; d_graph_pool_capacity = 0; return -1;
    }

    d_graph_pool_capacity = capacity;
    return 0;
}

static int ensure_phasedial_pool(uint32_t num_nodes) {
    if (d_phasedial_cache != nullptr && d_phasedial_capacity >= num_nodes) {
        return 0;
    }

    if (d_phasedial_cache) cudaFree(d_phasedial_cache);
    if (d_phasedial_query) cudaFree(d_phasedial_query);
    if (d_phasedial_sims) cudaFree(d_phasedial_sims);

    uint32_t capacity = num_nodes * 2;
    if (capacity < 256) capacity = 256;

    cudaError_t err = cudaMalloc((void**)&d_phasedial_cache, (size_t)capacity * 1024 * sizeof(double));
    if (err != cudaSuccess) { d_phasedial_cache = nullptr; d_phasedial_capacity = 0; return -1; }

    err = cudaMalloc((void**)&d_phasedial_query, 1024 * sizeof(double));
    if (err != cudaSuccess) {
        cudaFree(d_phasedial_cache); d_phasedial_cache = nullptr; d_phasedial_capacity = 0; return -1;
    }

    err = cudaMalloc((void**)&d_phasedial_sims, capacity * sizeof(double));
    if (err != cudaSuccess) {
        cudaFree(d_phasedial_cache); cudaFree(d_phasedial_query);
        d_phasedial_cache = nullptr; d_phasedial_capacity = 0; return -1;
    }

    d_phasedial_capacity = capacity;
    return 0;
}

static int ensure_encode_pool(uint32_t num_values) {
    if (d_encode_primes == nullptr) {
        cudaError_t err = cudaMalloc((void**)&d_encode_primes, 512 * sizeof(float));
        if (err != cudaSuccess) { d_encode_primes = nullptr; return -1; }

        err = cudaMalloc((void**)&d_encode_out, 1024 * sizeof(double));
        if (err != cudaSuccess) {
            cudaFree(d_encode_primes); d_encode_primes = nullptr; return -1;
        }
    }

    if (d_encode_phases != nullptr && d_encode_capacity >= num_values) {
        return 0;
    }

    if (d_encode_phases) cudaFree(d_encode_phases);

    uint32_t capacity = num_values * 2;
    if (capacity < 256) capacity = 256;

    cudaError_t err = cudaMalloc((void**)&d_encode_phases, capacity * sizeof(double));
    if (err != cudaSuccess) { d_encode_phases = nullptr; d_encode_capacity = 0; return -1; }

    d_encode_capacity = capacity;
    return 0;
}

static int ensure_blocks_pool(uint32_t num_values) {
    if (d_sums_pool == nullptr) {
        cudaError_t err = cudaMalloc((void**)&d_sums_pool, 4 * sizeof(double));
        if (err != cudaSuccess) { d_sums_pool = nullptr; return -1; }
    }

    if (d_blocks_pool != nullptr && d_blocks_capacity >= num_values) {
        return 0;
    }

    if (d_blocks_pool) cudaFree(d_blocks_pool);

    uint32_t capacity = num_values * 2;
    if (capacity < 256) capacity = 256;

    cudaError_t err = cudaMalloc((void**)&d_blocks_pool, (size_t)capacity * 8 * sizeof(uint64_t));
    if (err != cudaSuccess) { d_blocks_pool = nullptr; d_blocks_capacity = 0; return -1; }

    d_blocks_capacity = capacity;
    return 0;
}

static int ensure_bvp_pool(void) {
    if (d_bvp_allocated) return 0;

    cudaError_t err = cudaMalloc((void**)&d_bvp_start, 8 * sizeof(uint64_t));
    if (err != cudaSuccess) { d_bvp_start = nullptr; return -1; }

    err = cudaMalloc((void**)&d_bvp_goal, 8 * sizeof(uint64_t));
    if (err != cudaSuccess) { cudaFree(d_bvp_start); d_bvp_start = nullptr; return -1; }

    err = cudaMalloc((void**)&d_bvp_best, sizeof(unsigned long long));
    if (err != cudaSuccess) {
        cudaFree(d_bvp_start); cudaFree(d_bvp_goal);
        d_bvp_start = nullptr; d_bvp_goal = nullptr; return -1;
    }

    d_bvp_allocated = 1;
    return 0;
}

/* ─── Host dispatch functions ─────────────────────────────────────────── */

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
    if (ensure_phasedial_pool(num_nodes) != 0) return -1;

    size_t cache_bytes = (size_t)num_nodes * 1024 * sizeof(double);

    cudaError_t err = cudaMemcpy(d_phasedial_cache, cache_nodes_ptr, cache_bytes, cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_phasedial_query, query_dial_ptr, 1024 * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    resolve_phasedial_kernel<<<(num_nodes + 7) / 8, 256>>>(
        d_phasedial_cache, d_phasedial_query, d_phasedial_sims, num_nodes
    );

    err = cudaGetLastError();
    if (err != cudaSuccess) return -2;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return -2;

    err = cudaMemcpy(similarities_ptr, d_phasedial_sims, num_nodes * sizeof(double), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) return -3;

    return 0;
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
    if (ensure_encode_pool(num_values) != 0) return -1;

    cudaError_t err = cudaMemcpy(d_encode_phases, structural_phases_ptr, num_values * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_encode_primes, primes_ptr, 512 * sizeof(float), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    encode_phasedial_kernel<<<(512 + 255) / 256, 256>>>(
        d_encode_phases, d_encode_primes, d_encode_out, num_values
    );

    err = cudaGetLastError();
    if (err != cudaSuccess) return -2;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return -2;

    err = cudaMemcpy(out_dial_ptr, d_encode_out, 1024 * sizeof(double), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) return -3;

    return 0;
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
    if (ensure_blocks_pool(num_values) != 0) return -1;

    double zeros[4] = {0.0, 0.0, 0.0, 0.0};

    cudaError_t err = cudaMemcpy(d_blocks_pool, value_blocks_ptr, (size_t)num_values * 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_sums_pool, zeros, 4 * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    seq_toroidal_mean_phase_kernel<<<(num_values + 255) / 256, 256>>>(
        d_blocks_pool, d_sums_pool, num_values
    );

    err = cudaGetLastError();
    if (err != cudaSuccess) return -2;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return -2;

    double sums[4];
    err = cudaMemcpy(sums, d_sums_pool, 4 * sizeof(double), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) return -3;

    *out_theta = atan2(sums[0], sums[1]);
    *out_phi = atan2(sums[2], sums[3]);
    if (*out_phi < 0.0) *out_phi += 2.0 * M_PI;

    return 0;
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
    if (ensure_blocks_pool(num_values) != 0) return -1;

    double zeros[4] = {0.0, 0.0, 0.0, 0.0};

    cudaError_t err = cudaMemcpy(d_blocks_pool, value_blocks_ptr, (size_t)num_values * 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_sums_pool, zeros, 3 * sizeof(double), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    weighted_circular_mean_kernel<<<(num_values + 255) / 256, 256>>>(
        d_blocks_pool, d_sums_pool, num_values
    );

    err = cudaGetLastError();
    if (err != cudaSuccess) return -2;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return -2;

    double sums[3];
    err = cudaMemcpy(sums, d_sums_pool, 3 * sizeof(double), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) return -3;

    *out_phase = atan2(sums[0], sums[1]);
    if (*out_phase < 0.0) *out_phase += 2.0 * M_PI;
    double r = sqrt(sums[0] * sums[0] + sums[1] * sums[1]);
    *out_concentration = (sums[2] > 0.0) ? (r / sums[2]) : 0.0;

    return 0;
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
    if (ensure_bvp_pool() != 0) return -1;

    unsigned long long init = 0ULL;

    cudaError_t err = cudaMemcpy(d_bvp_start, start_blocks_ptr, 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_bvp_goal, goal_blocks_ptr, 8 * sizeof(uint64_t), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_bvp_best, &init, sizeof(unsigned long long), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    solve_bvp_kernel<<<(65792 + 255) / 256, 256>>>(d_bvp_start, d_bvp_goal, d_bvp_best);

    err = cudaGetLastError();
    if (err != cudaSuccess) return -2;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return -2;

    unsigned long long packed;
    err = cudaMemcpy(&packed, d_bvp_best, sizeof(unsigned long long), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) return -3;

    uint32_t id = (uint32_t)(packed & 0xFFFFFFFFULL);
    uint32_t match_count = (uint32_t)(packed >> 32);
    *out_scale = (uint16_t)((id / 257) + 1);
    *out_translate = (uint16_t)(id % 257);
    *out_distance = 1.0 - ((double)match_count / (double)257.0);

    return 0;
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

    cudaError_t err = cudaSetDevice(device_id);
    if (err != cudaSuccess) return -1;

    if (ensure_resonance_pool(num_nodes) != 0) return -1;

    unsigned long long initial_val = 0ULL;

    err = cudaMemcpy(d_graph_pool, graph_nodes_ptr, num_nodes * sizeof(GFRotation), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_ctx_pool, active_context_ptr, sizeof(GFRotation), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    err = cudaMemcpy(d_result_pool, &initial_val, sizeof(unsigned long long), cudaMemcpyHostToDevice);
    if (err != cudaSuccess) return -1;

    uint32_t threads_per_block = 256u;
    uint32_t blocks_per_grid = (num_nodes + threads_per_block - 1u) / threads_per_block;

    resolve_resonance_kernel<<<blocks_per_grid, threads_per_block>>>(
        d_graph_pool, d_ctx_pool, d_result_pool, num_nodes, 0u
    );

    err = cudaGetLastError();
    if (err != cudaSuccess) return -2;

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) return -2;

    err = cudaMemcpy(out_result, d_result_pool, sizeof(unsigned long long), cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) return -3;

    return 0;
}

void cleanup_cuda_pools(void) {
    if (d_graph_pool) { cudaFree(d_graph_pool); d_graph_pool = nullptr; }
    if (d_ctx_pool) { cudaFree(d_ctx_pool); d_ctx_pool = nullptr; }
    if (d_result_pool) { cudaFree(d_result_pool); d_result_pool = nullptr; }
    d_graph_pool_capacity = 0;

    if (d_phasedial_cache) { cudaFree(d_phasedial_cache); d_phasedial_cache = nullptr; }
    if (d_phasedial_query) { cudaFree(d_phasedial_query); d_phasedial_query = nullptr; }
    if (d_phasedial_sims) { cudaFree(d_phasedial_sims); d_phasedial_sims = nullptr; }
    d_phasedial_capacity = 0;

    if (d_encode_phases) { cudaFree(d_encode_phases); d_encode_phases = nullptr; }
    if (d_encode_primes) { cudaFree(d_encode_primes); d_encode_primes = nullptr; }
    if (d_encode_out) { cudaFree(d_encode_out); d_encode_out = nullptr; }
    d_encode_capacity = 0;

    if (d_blocks_pool) { cudaFree(d_blocks_pool); d_blocks_pool = nullptr; }
    if (d_sums_pool) { cudaFree(d_sums_pool); d_sums_pool = nullptr; }
    d_blocks_capacity = 0;

    if (d_bvp_start) { cudaFree(d_bvp_start); d_bvp_start = nullptr; }
    if (d_bvp_goal) { cudaFree(d_bvp_goal); d_bvp_goal = nullptr; }
    if (d_bvp_best) { cudaFree(d_bvp_best); d_bvp_best = nullptr; }
    d_bvp_allocated = 0;
}

} // extern "C"

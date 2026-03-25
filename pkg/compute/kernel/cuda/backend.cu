#include <cuda_runtime.h>
#include <stdint.h>

/*
    SIX NATIVE VM KERNELS (NVIDIA CUDA)
    
    All kernels operate on `primitive.Value` arrays: 128 uint64 words (8192 bits).
    The 8192nd bit (bit 63 of word 127) is the OOB Instruction Mask.
*/

static const uint32_t WORDS = 128;
static const uint32_t CORE_BITS = 8191;
static const uint64_t LAST_MASK = (1ULL << 63) - 1ULL; // 0x7FFFFFFFFFFFFFFF

#define TT_NOR 1u
#define TT_AND_NOT 2u
#define TT_NOT 3u
#define TT_CONVERSE_NONIMPLICATION 4u
#define TT_XOR 6u
#define TT_NAND 7u
#define TT_AND 8u
#define TT_XNOR 9u
#define TT_OR 14u

__device__ __forceinline__ ulonglong4 broadcast_u64(uint64_t m) {
    return make_ulonglong4(m, m, m, m);
}

__device__ __forceinline__ ulonglong4 v_and(const ulonglong4& a, const ulonglong4& b) {
    return make_ulonglong4(a.x & b.x, a.y & b.y, a.z & b.z, a.w & b.w);
}

__device__ __forceinline__ ulonglong4 v_xor(const ulonglong4& a, const ulonglong4& b) {
    return make_ulonglong4(a.x ^ b.x, a.y ^ b.y, a.z ^ b.z, a.w ^ b.w);
}

__global__ void universal_bitwise_kernel(
    const ulonglong4* A,
    const ulonglong4* B,
    ulonglong4* DST,
    uint32_t num_values,
    uint32_t op
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;
    uint32_t base = id * 32;

    uint64_t m0 = 0 - (uint64_t)(op & 1);
    uint64_t m1 = 0 - (uint64_t)((op >> 1) & 1);
    uint64_t m2 = 0 - (uint64_t)((op >> 2) & 1);
    uint64_t m3 = 0 - (uint64_t)((op >> 3) & 1);

    uint64_t k1 = m0 ^ m2;
    uint64_t k2 = m0 ^ m1;
    uint64_t k3 = m0 ^ m1 ^ m2 ^ m3;

    ulonglong4 Bk1 = broadcast_u64(k1);
    ulonglong4 Bk2 = broadcast_u64(k2);
    ulonglong4 Bk3 = broadcast_u64(k3);
    ulonglong4 Bm0 = broadcast_u64(m0);

    for (uint32_t i = 0; i < 31; i++) {
        ulonglong4 a = A[base + i];
        ulonglong4 b = B[base + i];
        ulonglong4 ab = v_and(a, b);
        DST[base + i] = v_xor(v_xor(v_xor(Bm0, v_and(Bk1, a)), v_and(Bk2, b)), v_and(Bk3, ab));
    }

    ulonglong4 a_last = A[base + 31];
    ulonglong4 b_last = B[base + 31];
    ulonglong4 ab_last = v_and(a_last, b_last);
    ulonglong4 dst_last = v_xor(v_xor(v_xor(Bm0, v_and(Bk1, a_last)), v_and(Bk2, b_last)), v_and(Bk3, ab_last));
    dst_last.w = a_last.w;

    DST[base + 31] = dst_last;
}


static uint64_t* d_pool_A = nullptr;
static uint64_t* d_pool_B = nullptr;
static uint64_t* d_pool_dst = nullptr;
static uint32_t pool_capacity = 0;

static int ensure_pool(uint32_t num_values) {
    if (pool_capacity >= num_values) return 0;

    if (d_pool_A) cudaFree(d_pool_A);
    if (d_pool_B) cudaFree(d_pool_B);
    if (d_pool_dst) cudaFree(d_pool_dst);

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    size_t bytes = cap * 1024; // 1024 bytes per Value
    if (cudaMalloc((void**)&d_pool_A, bytes) != cudaSuccess) return -1;
    if (cudaMalloc((void**)&d_pool_B, bytes) != cudaSuccess) return -1;
    if (cudaMalloc((void**)&d_pool_dst, bytes) != cudaSuccess) return -1;

    pool_capacity = cap;
    return 0;
}

extern "C" {

int cuda_device_count() {
    int count = 0;
    if (cudaGetDeviceCount(&count) != cudaSuccess) return 0;
    return count;
}

void cleanup_cuda_pools() {
    if (d_pool_A) { cudaFree(d_pool_A); d_pool_A = nullptr; }
    if (d_pool_B) { cudaFree(d_pool_B); d_pool_B = nullptr; }
    if (d_pool_dst) { cudaFree(d_pool_dst); d_pool_dst = nullptr; }
    pool_capacity = 0;
}

// ─── Exported Implementation ───────────────────────────────────────────

int universal_bitwise_cuda(int device_id, const void* a_host, const void* b_host, void* dst_host, uint32_t op, uint32_t num_values) {
    if (!a_host || !b_host || !dst_host || num_values == 0) return -1;
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;
    if (ensure_pool(num_values) != 0) return -1;
    size_t bytes = num_values * 1024;
    cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice);
    cudaMemcpy(d_pool_B, b_host, bytes, cudaMemcpyHostToDevice);
    int threads = 256;
    int blocks = (num_values + threads - 1) / threads;
    universal_bitwise_kernel<<<blocks, threads>>>((const ulonglong4*)d_pool_A, (const ulonglong4*)d_pool_B, (ulonglong4*)d_pool_dst, num_values, op);
    if (cudaGetLastError() != cudaSuccess) return -2;
    if (cudaDeviceSynchronize() != cudaSuccess) return -3;
    cudaMemcpy(dst_host, d_pool_dst, bytes, cudaMemcpyDeviceToHost);
    return 0;
}

} // extern "C"
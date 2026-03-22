#include <cuda_runtime.h>
#include <stdint.h>

/*
    SIX NATIVE VM KERNELS (NVIDIA CUDA)
    
    All kernels operate on `primitive.Value` arrays: 128 uint64 words (8192 bits).
    The first 8191 bits map to the GF(8191) Mersenne Prime Field.
    The 8192nd bit (bit 63 of word 127) is the OOB Instruction Mask.
*/

static const uint32_t WORDS = 128;
static const uint32_t CORE_BITS = 8191;
static const uint64_t LAST_MASK = (1ULL << 63) - 1ULL; // 0x7FFFFFFFFFFFFFFF

/* ─── GF(8191) Mersenne Prime Math ────────────────────────────────────── */

__device__ __forceinline__ uint32_t mod8191(uint32_t x) {
    x = (x >> 13) + (x & 8191);
    if (x >= 8191) x -= 8191;
    return x;
}

__device__ __forceinline__ void derive_motor(const uint64_t* val, uint32_t& s, uint32_t& t) {
    s = 1;
    t = 0;
    
    for (int i = 0; i < WORDS; i++) {
        uint64_t w = val[i];
        if (i == WORDS - 1) w &= LAST_MASK;
        
        while (w != 0) {
            int bit = __ffsll(w) - 1; // CUDA intrinsics: Find First Set (1-based)
            uint32_t p = i * 64 + bit;
            s = mod8191(s * p);
            t = mod8191(t + p);
            w &= w - 1ULL;
        }
    }
    
    if (s == 0) s = 1;
}

__device__ __forceinline__ void invert_motor(uint32_t scale, uint32_t translate, uint32_t& invScale, uint32_t& invTranslate) {
    int t_val = 0, newT = 1;
    int r = 8191, newR = (int)scale;

    while (newR != 0) {
        int quotient = r / newR;

        int tempT = t_val - quotient * newT;
        t_val = newT;
        newT = tempT;

        int tempR = r - quotient * newR;
        r = newR;
        newR = tempR;
    }

    if (t_val < 0) t_val += 8191;
    
    invScale = (uint32_t)t_val;
    invTranslate = (8191 - (invScale * translate) % 8191) % 8191;
}

/* ─── Vectorized Bitwise Kernels ──────────────────────────────────────── */

// We use ulonglong4 (256-bit vectors) to maximize PCIe / Memory Bus bandwidth.
// 1024 bytes = 32 ulonglong4 loops per thread.

#define BITWISE_KERNEL(name, OP_EXPR) \
__global__ void bitwise_##name##_kernel(const ulonglong4* A, const ulonglong4* B, ulonglong4* dst, uint32_t num_values) { \
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x; \
    if (id >= num_values) return; \
    uint32_t base = id * 32; \
    for (int i = 0; i < 32; i++) { \
        ulonglong4 a = A[base + i]; \
        ulonglong4 b = B[base + i]; \
        ulonglong4 d; \
        d.x = OP_EXPR(a.x, b.x); \
        d.y = OP_EXPR(a.y, b.y); \
        d.z = OP_EXPR(a.z, b.z); \
        d.w = OP_EXPR(a.w, b.w); \
        if (i == 31) d.w &= LAST_MASK; \
        dst[base + i] = d; \
    } \
}

#define OP_OR(a,b) ((a) | (b))
#define OP_AND(a,b) ((a) & (b))
#define OP_XOR(a,b) ((a) ^ (b))
#define OP_ANDNOT(a,b) ((a) & ~(b))
#define OP_NAND(a,b) (~((a) & (b)))
#define OP_NOR(a,b) (~((a) | (b)))
#define OP_XNOR(a,b) (~((a) ^ (b)))
#define OP_CNI(a,b) ((b) & ~(a))

BITWISE_KERNEL(or, OP_OR)
BITWISE_KERNEL(and, OP_AND)
BITWISE_KERNEL(xor, OP_XOR)
BITWISE_KERNEL(and_not, OP_ANDNOT)
BITWISE_KERNEL(nand, OP_NAND)
BITWISE_KERNEL(nor, OP_NOR)
BITWISE_KERNEL(xnor, OP_XNOR)
BITWISE_KERNEL(converse_nonimplication, OP_CNI)

__global__ void bitwise_not_kernel(const ulonglong4* A, ulonglong4* dst, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;
    uint32_t base = id * 32;
    for (int i = 0; i < 32; i++) {
        ulonglong4 a = A[base + i];
        ulonglong4 d;
        d.x = ~a.x; d.y = ~a.y; d.z = ~a.z; d.w = ~a.w;
        if (i == 31) d.w &= LAST_MASK;
        dst[base + i] = d;
    }
}


/* ─── Affine Motor Kernels ────────────────────────────────────────────── */

__global__ void motor_apply_kernel(const uint64_t* A, const uint64_t* B, uint64_t* dst, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;
    uint32_t base = id * WORDS;

    uint32_t s, t;
    derive_motor(A + base, s, t);

    uint64_t tmp[WORDS] = {0};

    for (int i = 0; i < WORDS; i++) {
        uint64_t w = B[base + i];
        if (i == WORDS - 1) w &= LAST_MASK;

        while (w != 0) {
            int bit = __ffsll(w) - 1;
            uint32_t p = i * 64 + bit;
            uint32_t mapped = mod8191(s * p + t);
            tmp[mapped / 64] |= (1ULL << (mapped % 64));
            w &= w - 1ULL;
        }
    }

    for (int i = 0; i < WORDS; i++) dst[base + i] = tmp[i];
}

__global__ void motor_invert_kernel(const uint64_t* A, const uint64_t* B, uint64_t* dst, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;
    uint32_t base = id * WORDS;

    uint32_t s, t, invS, invT;
    derive_motor(A + base, s, t);
    invert_motor(s, t, invS, invT);

    uint64_t tmp[WORDS] = {0};

    for (int i = 0; i < WORDS; i++) {
        uint64_t w = B[base + i];
        if (i == WORDS - 1) w &= LAST_MASK;

        while (w != 0) {
            int bit = __ffsll(w) - 1;
            uint32_t p = i * 64 + bit;
            uint32_t mapped = mod8191(invS * p + invT);
            tmp[mapped / 64] |= (1ULL << (mapped % 64));
            w &= w - 1ULL;
        }
    }

    for (int i = 0; i < WORDS; i++) dst[base + i] = tmp[i];
}

__global__ void motor_compose_kernel(const uint64_t* A, const uint64_t* B, uint64_t* dst, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;
    uint32_t base = id * WORDS;

    uint32_t sA, tA, sB, tB;
    derive_motor(A + base, sA, tA);
    derive_motor(B + base, sB, tB);

    uint32_t comp_s = mod8191(sB * sA);
    uint32_t comp_t = mod8191(sB * tA + tB);

    uint64_t tmp[WORDS] = {0};

    for (int i = 0; i < WORDS; i++) {
        uint64_t w = B[base + i];
        if (i == WORDS - 1) w &= LAST_MASK;

        while (w != 0) {
            int bit = __ffsll(w) - 1;
            uint32_t p = i * 64 + bit;
            uint32_t mapped = mod8191(comp_s * p + comp_t);
            tmp[mapped / 64] |= (1ULL << (mapped % 64));
            w &= w - 1ULL;
        }
    }

    for (int i = 0; i < WORDS; i++) dst[base + i] = tmp[i];
}

/* ─── Structural Geometry Kernels ─────────────────────────────────────── */

__global__ void roll_left_kernel(const uint64_t* src, uint64_t* dst, uint32_t shift_amount, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;
    uint32_t base = id * WORDS;

    uint32_t s = ((shift_amount % CORE_BITS) + CORE_BITS) % CORE_BITS;
    if (s == 0) {
        for (int i = 0; i < WORDS; i++) dst[base + i] = src[base + i];
        return;
    }

    uint32_t r = CORE_BITS - s;
    uint32_t wShiftL = s / 64;
    uint32_t bShiftL = s % 64;
    uint32_t wShiftR = r / 64;
    uint32_t bShiftR = r % 64;

    uint64_t tmp[WORDS] = {0};

    if (bShiftL == 0) {
        for(uint32_t i = wShiftL; i < WORDS; i++) tmp[i] = src[base + i - wShiftL];
    } else {
        tmp[wShiftL] = src[base + 0] << bShiftL;
        for(uint32_t i = wShiftL + 1; i < WORDS; i++) {
            tmp[i] = (src[base + i - wShiftL] << bShiftL) | (src[base + i - wShiftL - 1] >> (64 - bShiftL));
        }
    }

    if (bShiftR == 0) {
        for(uint32_t i = 0; i < WORDS - wShiftR; i++) tmp[i] |= src[base + i + wShiftR];
    } else {
        for(uint32_t i = 0; i < WORDS - wShiftR - 1; i++) {
            tmp[i] |= (src[base + i + wShiftR] >> bShiftR) | (src[base + i + wShiftR + 1] << (64 - bShiftR));
        }
        tmp[WORDS - 1 - wShiftR] |= src[base + WORDS - 1] >> bShiftR;
    }

    tmp[WORDS - 1] &= LAST_MASK;

    for (int i = 0; i < WORDS; i++) dst[base + i] = tmp[i];
}


/* ─── Host VRAM Pools & Dispatch (C API for Go) ───────────────────────── */

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

// ─── Dispatch Macros ───────────────────────────────────────────────────

#define LAUNCH_BITWISE(NAME) \
int bitwise_##NAME##_cuda(int device_id, const void* a_host, const void* b_host, void* dst_host, uint32_t num_values) { \
    if (!a_host || !dst_host || num_values == 0) return -1; \
    if (cudaSetDevice(device_id) != cudaSuccess) return -1; \
    if (ensure_pool(num_values) != 0) return -1; \
    size_t bytes = num_values * 1024; \
    cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice); \
    if (b_host) cudaMemcpy(d_pool_B, b_host, bytes, cudaMemcpyHostToDevice); \
    int threads = 256; \
    int blocks = (num_values + threads - 1) / threads; \
    bitwise_##NAME##_kernel<<<blocks, threads>>>((const ulonglong4*)d_pool_A, (const ulonglong4*)d_pool_B, (ulonglong4*)d_pool_dst, num_values); \
    if (cudaGetLastError() != cudaSuccess) return -2; \
    if (cudaDeviceSynchronize() != cudaSuccess) return -3; \
    cudaMemcpy(dst_host, d_pool_dst, bytes, cudaMemcpyDeviceToHost); \
    return 0; \
}

#define LAUNCH_MOTOR(NAME) \
int motor_##NAME##_cuda(int device_id, const void* a_host, const void* b_host, void* dst_host, uint32_t num_values) { \
    if (!a_host || !b_host || !dst_host || num_values == 0) return -1; \
    if (cudaSetDevice(device_id) != cudaSuccess) return -1; \
    if (ensure_pool(num_values) != 0) return -1; \
    size_t bytes = num_values * 1024; \
    cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice); \
    cudaMemcpy(d_pool_B, b_host, bytes, cudaMemcpyHostToDevice); \
    int threads = 256; \
    int blocks = (num_values + threads - 1) / threads; \
    motor_##NAME##_kernel<<<blocks, threads>>>((const uint64_t*)d_pool_A, (const uint64_t*)d_pool_B, (uint64_t*)d_pool_dst, num_values); \
    if (cudaGetLastError() != cudaSuccess) return -2; \
    if (cudaDeviceSynchronize() != cudaSuccess) return -3; \
    cudaMemcpy(dst_host, d_pool_dst, bytes, cudaMemcpyDeviceToHost); \
    return 0; \
}

// ─── Exported Implementations ──────────────────────────────────────────

LAUNCH_BITWISE(or)
LAUNCH_BITWISE(and)
LAUNCH_BITWISE(xor)
LAUNCH_BITWISE(and_not)
LAUNCH_BITWISE(nand)
LAUNCH_BITWISE(nor)
LAUNCH_BITWISE(xnor)
LAUNCH_BITWISE(converse_nonimplication)

int bitwise_not_cuda(int device_id, const void* a_host, void* dst_host, uint32_t num_values) {
    if (!a_host || !dst_host || num_values == 0) return -1;
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;
    if (ensure_pool(num_values) != 0) return -1;
    size_t bytes = num_values * 1024;
    cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice);
    int threads = 256;
    int blocks = (num_values + threads - 1) / threads;
    bitwise_not_kernel<<<blocks, threads>>>((const ulonglong4*)d_pool_A, (ulonglong4*)d_pool_dst, num_values);
    if (cudaGetLastError() != cudaSuccess) return -2;
    if (cudaDeviceSynchronize() != cudaSuccess) return -3;
    cudaMemcpy(dst_host, d_pool_dst, bytes, cudaMemcpyDeviceToHost);
    return 0;
}

LAUNCH_MOTOR(apply)
LAUNCH_MOTOR(invert)
LAUNCH_MOTOR(compose)

int roll_left_cuda(int device_id, const void* src_host, void* dst_host, uint32_t shift, uint32_t num_values) {
    if (!src_host || !dst_host || num_values == 0) return -1;
    if (cudaSetDevice(device_id) != cudaSuccess) return -1;
    if (ensure_pool(num_values) != 0) return -1;
    size_t bytes = num_values * 1024;
    cudaMemcpy(d_pool_A, src_host, bytes, cudaMemcpyHostToDevice);
    int threads = 256;
    int blocks = (num_values + threads - 1) / threads;
    roll_left_kernel<<<blocks, threads>>>((const uint64_t*)d_pool_A, (uint64_t*)d_pool_dst, shift, num_values);
    if (cudaGetLastError() != cudaSuccess) return -2;
    if (cudaDeviceSynchronize() != cudaSuccess) return -3;
    cudaMemcpy(dst_host, d_pool_dst, bytes, cudaMemcpyDeviceToHost);
    return 0;
}

} // extern "C"
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

/*
get_program_op decodes a 4-bit opcode at position pc from a ulonglong4 that
packs 64 opcodes (16 per uint64 lane, 4 bits each).

  pc 0..15  → prog.x
  pc 16..31 → prog.y
  pc 32..47 → prog.z
  pc 48..63 → prog.w
*/
__device__ __forceinline__ uint32_t get_program_op(const ulonglong4& prog, int pc) {
    int word_idx = pc / 16;
    int shift    = (pc % 16) * 4;
    if (word_idx == 0) return (uint32_t)((prog.x >> shift) & 0xF);
    if (word_idx == 1) return (uint32_t)((prog.y >> shift) & 0xF);
    if (word_idx == 2) return (uint32_t)((prog.z >> shift) & 0xF);
    return                    (uint32_t)((prog.w >> shift) & 0xF);
}

/*
unified_bitwise_kernel — per-Value programmable ALU.

Each thread owns exactly one Value (32 × ulonglong4 = 1024 bytes).

1. Fetch the 64-op program stored in Region 3 of Value A.
   Region 3 starts at bit 4352 = word 68 = ulonglong4 index 17 in each Value.
2. Load the 15 ulonglong4s of Region 0 data (words 0-59) into registers.
3. Execute up to 64 program ticks, halting at the first opcode == 0.
   Each tick applies the full truth-table ALU against B's Region 0 data
   in-place on the working buffer (output of tick n is input to tick n+1).
4. Write the evolved Region 0 back to DST.  Word 60 (ulonglong4 index 15,
   lane x) holds the Instruction Register — clear its 4-bit opcode field
   before passing through.  All remaining metadata/affinity/program words
   are passed through from A unchanged.
*/
__global__ void unified_bitwise_kernel(
    const ulonglong4* A,
    const ulonglong4* B,
    ulonglong4* DST,
    uint32_t num_values
) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    // Each Value is exactly 32 ulonglong4s (128 × uint64).
    uint32_t base = id * 32;

    // ── Step 1: fetch the 64-op program from Region 3 (ulonglong4 index 17) ──
    ulonglong4 prog = A[base + 17];

    // ── Step 2: load Region 0 data (15 ulonglong4s = 60 words) ──────────────
    ulonglong4 work_A[15];
    #pragma unroll
    for (int i = 0; i < 15; i++) {
        work_A[i] = A[base + i];
    }

    // ── Step 3: execute the program (up to 64 ticks, halt on opcode 0) ──────
    for (int pc = 0; pc < 64; pc++) {
        uint32_t op = get_program_op(prog, pc);
        if (op == 0) break; // NOP / HALT

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

        #pragma unroll
        for (int i = 0; i < 15; i++) {
            ulonglong4 b_val = B[base + i];
            ulonglong4 ab    = v_and(work_A[i], b_val);
            work_A[i] = v_xor(v_xor(v_xor(
                Bm0, v_and(Bk1, work_A[i])
            ), v_and(Bk2, b_val)), v_and(Bk3, ab));
        }
    }

    // ── Step 4: write evolved Region 0 + pass-through metadata back to DST ──
    #pragma unroll
    for (int i = 0; i < 32; i++) {
        if (i < 15) {
            // Evolved Region 0 data.
            DST[base + i] = work_A[i];
        } else if (i == 15) {
            // ulonglong4 index 15: lane x holds word 60 (Instruction Register).
            // Clear the 4 low bits of lane x so the opcode is consumed.
            ulonglong4 meta = A[base + i];
            meta.x &= ~((uint64_t)0xF); // clear bits [3:0] of word 60
            DST[base + i] = meta;
        } else {
            // Pass through: ValueID, affinity, program, links, gossip, TTL.
            DST[base + i] = A[base + i];
        }
    }
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

    /*
    unified_bitwise_cuda — single entry point for Go.

    Copies `num_values` Value frames (1024 bytes each) to device memory,
    launches unified_bitwise_kernel so each Value executes its own in-band
    program, then copies results back.  The `op` parameter of the old
    universal_bitwise_cuda is gone; dispatch is now fully in-band.
    */
    int unified_bitwise_cuda(
        int device_id,
        const void* a_host,
        const void* b_host,
        void* dst_host,
        uint32_t num_values
    ) {
        if (!a_host || !b_host || !dst_host || num_values == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_pool(num_values) != 0) return -1;

        size_t bytes = num_values * 1024;

        cudaMemcpy(d_pool_A,   a_host, bytes, cudaMemcpyHostToDevice);
        cudaMemcpy(d_pool_B,   b_host, bytes, cudaMemcpyHostToDevice);

        int threads = 256;
        int blocks  = (num_values + threads - 1) / threads;

        unified_bitwise_kernel<<<blocks, threads>>>(
            (const ulonglong4*)d_pool_A,
            (const ulonglong4*)d_pool_B,
            (ulonglong4*)d_pool_dst,
            num_values
        );

        if (cudaGetLastError()    != cudaSuccess) return -2;
        if (cudaDeviceSynchronize() != cudaSuccess) return -3;

        cudaMemcpy(dst_host, d_pool_dst, bytes, cudaMemcpyDeviceToHost);
        return 0;
    }

} // extern "C"
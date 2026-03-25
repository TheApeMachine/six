#include <metal_stdlib>
using namespace metal;

/*
SIX NATIVE VM KERNELS (APPLE SILICON MSL)

All kernels operate on `primitive.Value` arrays: 128 ulong words (8192 bits).
The 8192nd bit (bit 63 of word 127) is the OOB Instruction Mask.
*/

/*
1. UNIVERSAL BITWISE ALU
Single kernel replaces all 9 individual bitwise kernels.
The 4-bit opcode encodes the truth-table row; mask expansion
produces the correct gate for all 16 boolean functions.
*/

kernel void universal_bitwise_kernel(
    device const ulong4* A   [[buffer(0)]],
    device const ulong4* B   [[buffer(1)]],
    device       ulong4* DST [[buffer(2)]],
    constant     uchar&  op  [[buffer(3)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * 32;

    // Expand truth table to 64-bit masks (applied across all 4 vector lanes)
    ulong m0 = 0 - (ulong)(op & 1);
    ulong m1 = 0 - (ulong)((op >> 1) & 1);
    ulong m2 = 0 - (ulong)((op >> 2) & 1);
    ulong m3 = 0 - (ulong)((op >> 3) & 1);

    ulong k1 = m0 ^ m2;
    ulong k2 = m0 ^ m1;
    ulong k3 = m0 ^ m1 ^ m2 ^ m3;

    // Process the first 31 vectors (Words 0 to 123)
    for (uint i = 0; i < 31; i++) {
        ulong4 a = A[base + i];
        ulong4 b = B[base + i];
        DST[base + i] = m0 ^ (k1 & a) ^ (k2 & b) ^ (k3 & (a & b));
    }

    // Process the 32nd vector (Words 124, 125, 126, and 127[Meta])
    ulong4 a_last = A[base + 31];
    ulong4 b_last = B[base + 31];
    ulong4 dst_last = m0 ^ (k1 & a_last) ^ (k2 & b_last) ^ (k3 & (a_last & b_last));

    // Override the final word (127) to preserve Particle A's Meta behavior
    dst_last.w = a_last.w; 

    DST[base + 31] = dst_last;
}

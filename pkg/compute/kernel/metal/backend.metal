#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

/*
SIX NATIVE VM KERNELS (APPLE SILICON MSL)

All kernels operate on physical primitive.Value matrix slices (128 words, 1024 bytes).
The 8192nd bit (bit 63 of word 127) is the Tombstone execution interrupt.
*/

// resolve_window geometrically bounds operation domains dynamically 
// by intercepting predefined Register pointers.
template <typename T>
inline uint2 resolve_window(uint ptr, uint default_len, T src) {
    if (ptr >= REG0 && ptr <= REG6) {
        ulong reg_val = src[ptr];
        uint start_idx = (uint)(reg_val >> 32);
        uint length_val = (uint)(reg_val & 0xFFFFFFFF);
        return uint2(start_idx, length_val);
    }
    return uint2(ptr, default_len);
}

/*
1. UNIFIED BITWISE ALU — completely aligned Register Kernel
*/
kernel void unified_bitwise_kernel(
    device const ulong* A   [[buffer(0)]],
    device const ulong* B   [[buffer(1)]],
    device       ulong* DST [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    // Each Value is perfectly 128 ulong words.
    uint base = id * WORDS;

    // ── Step 1: Copy passive Receiver B into mutable scratchpad memory ──
    ulong work_words[WORDS];
    for (int i = 0; i < WORDS; i++) {
        work_words[i] = B[base + i];
    }

    // ── Step 2: execute the microcode program strictly (8 ticks max) ────
    for (int pc = 0; pc < 8; pc++) {
        uint word_offset = (REGION_PROGRAM_START_BIT / 64) + (pc / 2);
        uint shift = (pc % 2) * 32;
        
        // Read native 32-bit executable firmware slice out of the graph memory
        uint instr = (uint)(A[base + word_offset] >> shift);
        
        uchar opBits = instr & 0xF;
        if (opBits == OP_HALT) {
            break;
        }

        uint src1  = (instr >> 4)  & 0xFF;
        uint src2  = (instr >> 12) & 0xFF;
        uint dest  = (instr >> 20) & 0xFF;
        uint len   = (instr >> 28) & 0xF;

        if (len == 0) {
            len = 1; // Operational boundary initialization
        }

        // Expand the literal 4-bit Universal Logic matrix to 64-Bit truth gates
        ulong m0 = 0 - (ulong)(opBits & 1);
        ulong m1 = 0 - (ulong)((opBits >> 1) & 1);
        ulong m2 = 0 - (ulong)((opBits >> 2) & 1);
        ulong m3 = 0 - (ulong)((opBits >> 3) & 1);

        ulong k1 = m0 ^ m2;
        ulong k2 = m0 ^ m1;
        ulong k3 = m0 ^ m1 ^ m2 ^ m3;

        // Perform Substrate Pointer Mapping natively inside GPU cache
        uint2 s1 = resolve_window(src1, len, &A[base]);
        uint2 s2 = resolve_window(src2, len, work_words);
        uint2 d  = resolve_window(dest, len, work_words);

        // Guard topological buffer boundaries
        uint op_len = s1.y;
        if (s2.y < op_len) op_len = s2.y;
        if (d.y < op_len) op_len = d.y;

        // Geometrically apply Boolean ALUs across the window parameters
        for (uint l = 0; l < op_len; l++) {
            uint s1_idx = s1.x + l;
            uint s2_idx = s2.x + l;
            uint d_idx  = d.x  + l;

            if (s1_idx >= WORDS || s2_idx >= WORDS || d_idx >= WORDS) {
                continue; // Guard hardware exceptions
            }

            ulong left  = A[base + s1_idx];
            ulong right = work_words[s2_idx];

            work_words[d_idx] = m0 ^ (k1 & left) ^ (k2 & right) ^ (k3 & (left & right));
        }
    }

    // ── Step 3: flush memory states from Scratchpad to native RAM (DST) ────
    for (int i = 0; i < WORDS; i++) {
        DST[base + i] = work_words[i];
    }
    
    // Mathematically wipe Program Instruction Register zero from persistent destination layer
    DST[base + (REGION_PROGRAM_START_BIT / 64)] &= 0xFFFFFFFF00000000ull; 
}

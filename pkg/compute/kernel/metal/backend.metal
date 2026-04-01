#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

kernel void unified_bitwise_kernel(
    device ulong* A         [[buffer(0)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    ulong ctx[WORDS];
    for (int i = 0; i < WORDS; i++) {
        ctx[i] = A[base + i];
    }

    for (uint slot = 0; slot < MAX_PC; slot++) {
        uint wordPos = PROGRAM_INDEX_WORD + (slot / 2);
        if (wordPos >= WORDS) {
            break;
        }

        uint shift = (slot % 2) * 32;
        uint instr = (uint)(ctx[wordPos] >> shift);
        if (instr == 0) {
            continue;
        }

        uchar op = instr & 0xF;
        uint srcWord = ((instr >> 4) & 0x3FFF) & 127;
        uint dstWord = ((instr >> 18) & 0x3FFF) & 127;

        ulong src = ctx[srcWord];
        ulong dst = ctx[dstWord];

        ulong m0 = 0 - (ulong)((op >> 3) & 1);
        ulong m1 = 0 - (ulong)((op >> 2) & 1);
        ulong m2 = 0 - (ulong)((op >> 1) & 1);
        ulong m3 = 0 - (ulong)(op & 1);

        ctx[dstWord] = m0 ^
            ((m0 ^ m2) & src) ^
            ((m0 ^ m1) & dst) ^
            ((m0 ^ m1 ^ m2 ^ m3) & (src & dst));
    }

    for (int i = 0; i < WORDS; i++) {
        A[base + i] = ctx[i];
    }
}


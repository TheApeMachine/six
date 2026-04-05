#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

/*
UniversalBitwise kernel — Metal implementation.

Per thread (one Value):
  1. Copy A (4 words) and B (4 words) from Tokens region.
  2. Expand B into 16 rotations × 4 words = 64-word surface.
     A is tiled 16 times to match.
  3. Extract one 4-bit opcode per rotation from Program region.
  4. Apply truth table across the full 64-element surface.
  5. Pack low 8 bits of each result into 8-word Signals region.

Token and Program regions are never mutated.
*/

kernel void unified_bitwise_kernel(
    device ulong* A [[buffer(0)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;

    // Load A (steady) and B (will be rotated).
    ulong a[A_WORDS];
    ulong b[B_WORDS];
    for (int i = 0; i < A_WORDS; i++) {
        a[i] = A[base + TOKENS_START_WORD + i];
    }
    for (int i = 0; i < B_WORDS; i++) {
        b[i] = A[base + TOKENS_START_WORD + A_WORDS + i];
    }

    // Load program region.
    ulong prog[PROGRAM_WORDS];
    for (int i = 0; i < PROGRAM_WORDS; i++) {
        prog[i] = A[base + PROGRAM_START_WORD + i];
    }

    // Expand surfaces, apply truth table, pack signals.
    ulong signals[SIGNALS_WORDS];
    for (int i = 0; i < SIGNALS_WORDS; i++) {
        signals[i] = 0;
    }

    for (int rot = 0; rot < NUM_ROTATIONS; rot++) {
        // Extract 4-bit opcode for this rotation.
        uint wordIdx = rot / 2;
        uint shift = (rot % 2) * 32;
        uchar op = (uchar)((prog[wordIdx] >> shift) & 0xF);

        // Build masks from truth table bits.
        ulong m0 = 0 - (ulong)(op & 1);         // bit 0: a=0,b=0
        ulong m1 = 0 - (ulong)((op >> 1) & 1);  // bit 1: a=1,b=0
        ulong m2 = 0 - (ulong)((op >> 2) & 1);  // bit 2: a=0,b=1
        ulong m3 = 0 - (ulong)((op >> 3) & 1);  // bit 3: a=1,b=1

        for (int w = 0; w < A_WORDS; w++) {
            // Apply truth table: result = (~a&~b&m0) | (a&~b&m1) | (~a&b&m2) | (a&b&m3)
            ulong av = a[w];
            ulong bv = b[w];
            ulong result = (~av & ~bv & m0) |
                           ( av & ~bv & m1) |
                           (~av &  bv & m2) |
                           ( av &  bv & m3);

            // Pack low 8 bits into signals.
            int sigIdx = rot * A_WORDS + w;  // 0..63
            int sigWord = sigIdx / 8;
            int sigShift = (sigIdx % 8) * 8;
            signals[sigWord] |= ((result & 0xFF) << sigShift);
        }

        // Rotate B left by 8 bits for next rotation.
        for (int w = 0; w < B_WORDS; w++) {
            b[w] = (b[w] << 8) | (b[w] >> 56);
        }
    }

    // Write only the Signals region.
    for (int i = 0; i < SIGNALS_WORDS; i++) {
        A[base + SIGNALS_START_WORD + i] = signals[i];
    }
}

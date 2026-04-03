#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

inline ulong rotl64(ulong x, int r) {
    r &= 63;
    return (x << r) | (x >> (64 - r));
}

inline ulong majority_u64(ulong a, ulong b, ulong c) {
    return (a & b) | (b & c) | (a & c);
}

void execute_extended_slot(thread ulong* ctx, uint instr) {
    if ((instr & 0x80000000u) == 0u) {
        return;
    }
    uint op = instr & 0x7Fu;
    int argA = (int)((instr >> 7) & 0x7Fu);
    int argB = (int)((instr >> 14) & 0x7Fu);
    int argC = (int)((instr >> 21) & 0x7Fu);

    switch (op) {
    case 1u:
        if (argA < 0 || argA + TOKEN_WORDS > WORDS ||
            TOKENS_START_WORD < 0 || TOKENS_START_WORD + TOKEN_WORDS > WORDS) {
            break;
        }
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int wi = TOKENS_START_WORD + i;
            int ai = argA + i;
            ctx[wi] ^= ctx[ai];
        }
        break;
    case 2u:
        if (argA < 0 || argA + TOKEN_WORDS > WORDS ||
            argB < 0 || argB + TOKEN_WORDS > WORDS ||
            TOKENS_START_WORD < 0 || TOKENS_START_WORD + TOKEN_WORDS > WORDS) {
            break;
        }
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int wi = TOKENS_START_WORD + i;
            int ai = argA + i;
            int bi = argB + i;
            ctx[wi] = majority_u64(ctx[wi], ctx[ai], ctx[bi]);
        }
        break;
    case 3u: {
        int rot = argC & 63;
        if (rot == 0) {
            break;
        }
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int wi = TOKENS_START_WORD + i;
            ctx[wi] = rotl64(ctx[wi], rot);
        }
        break;
    }
    case 4u: {
        if (TOKEN_WORDS <= 1) {
            break;
        }
        int sh = argC % TOKEN_WORDS;
        if (sh == 0) {
            break;
        }
        if (TOKENS_START_WORD < 0 || TOKENS_START_WORD + TOKEN_WORDS > WORDS) {
            break;
        }
        ulong buf[TOKEN_WORDS];
        for (int i = 0; i < TOKEN_WORDS; i++) {
            buf[i] = ctx[TOKENS_START_WORD + i];
        }
        for (int i = 0; i < TOKEN_WORDS; i++) {
            int target = (i + sh) % TOKEN_WORDS;
            ctx[TOKENS_START_WORD + target] = buf[i];
        }
        break;
    }
    case 5u:
        ctx[STATE_INDEX_WORD] = 0x4D4D4C44UL;
        ctx[STATE_ACCUM_WORD] =
            (ulong)(argA & 0x7F) | ((ulong)(argB & 0x7F) << 8);
        break;
    case 6u:
        break;
    default:
        break;
    }
}

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

        if (instr & 0x80000000u) {
            execute_extended_slot(ctx, instr);
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


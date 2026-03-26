#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

kernel void unified_bitwise_kernel(
    device const ulong* A   [[buffer(0)]],
    device const ulong* B   [[buffer(1)]],
    device       ulong* DST [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;

    ulong contexts[2][128];
    for (int i = 0; i < WORDS; i++) {
        contexts[0][i] = A[base + i];
        contexts[1][i] = B[base + i];
    }

    while (true) {
        ulong pc = contexts[0][REG_PC];
        if (pc >= MAX_PC) {
            break;
        }

        ulong wordPos = PROGRAM_INDEX_WORD + (pc / 2);
        uint shift = (pc % 2) * 32;
        uint instr = (uint)(contexts[0][wordPos] >> shift);

        uchar op = instr & 0xF;
        if (op == 0 && pc > 0) {
            break;
        }

        uint16_t srcCode = (instr >> 4) & 0x3FFF;
        uint16_t dstCode = (instr >> 18) & 0x3FFF;

        contexts[0][REG_PC]++;

        ulong srcVal = 0;
        bool sSpan = false;
        if ((srcCode & 0x1000) != 0) {
            srcVal = (ulong)(srcCode & 0x0FFF);
            sSpan = true;
        } else if ((srcCode & 0x2000) != 0) {
            srcVal = contexts[0][srcCode & 0x0FFF];
        } else {
            srcVal = (ulong)srcCode;
        }

        ulong dstVal = 0;
        bool dSpan = false;
        if ((dstCode & 0x1000) != 0) {
            dstVal = (ulong)(dstCode & 0x0FFF);
            dSpan = true;
        } else if ((dstCode & 0x2000) != 0) {
            dstVal = contexts[0][dstCode & 0x0FFF];
        } else {
            dstVal = (ulong)dstCode;
        }

        if (sSpan || dSpan) {
            ulong sBase = srcVal;
            ulong sCtx = contexts[0][sBase];
            ulong sOff = contexts[0][sBase+1];
            ulong sLen = contexts[0][sBase+2];

            ulong dBase = dstVal;
            ulong dCtx = contexts[0][dBase];
            ulong dOff = contexts[0][dBase+1];
            ulong dLen = contexts[0][dBase+2];

            ulong limit = (sLen < dLen) ? sLen : dLen;
            for (ulong i = 0; i < limit; i++) {
                ulong sWord = (sOff + i) / 64;
                ulong sBit = (sOff + i) % 64;
                uchar sb = 0;
                if (sWord < WORDS) sb = (contexts[sCtx % 2][sWord] >> sBit) & 1;

                ulong dWord = (dOff + i) / 64;
                ulong dBit = (dOff + i) % 64;
                uchar db = 0;
                if (dWord < WORDS) db = (contexts[dCtx % 2][dWord] >> dBit) & 1;

                uint idx = (1 - db) | ((1 - sb) << 1);
                uchar res = (op >> idx) & 1;

                if (dWord < WORDS) {
                    if (res == 1) {
                        contexts[dCtx % 2][dWord] |= (1ULL << dBit);
                    } else {
                        contexts[dCtx % 2][dWord] &= ~(1ULL << dBit);
                    }
                }
            }
            continue;
        }

        uint dstIdx = dstCode & 0x0FFF;
        ulong left = srcVal;
        ulong right = dstVal;

        ulong m0 = 0 - (ulong)((op >> 3) & 1);
        ulong m1 = 0 - (ulong)((op >> 2) & 1);
        ulong m2 = 0 - (ulong)((op >> 1) & 1);
        ulong m3 = 0 - (ulong)(op & 1);

        ulong res = m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right));
        contexts[0][dstIdx] = res;
    }

    for (int i = 0; i < WORDS; i++) {
        DST[base + i] = contexts[0][i];
    }
}

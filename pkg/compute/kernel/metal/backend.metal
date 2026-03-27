#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

#define MARK_EXEC_EXIT(ctx, code) do { \
    (ctx)[EXEC_STATUS_WORD] &= 0x0000FFFFFFFFFFFFUL; \
    (ctx)[EXEC_STATUS_WORD] |= ((ulong)(code) << EXEC_STATUS_SHIFT); \
} while (0)

kernel void unified_bitwise_kernel(
    device ulong* A         [[buffer(0)]],
    device const ulong* B   [[buffer(1)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;

    ulong contexts[2][128];
    for (int i = 0; i < WORDS; i++) {
        contexts[0][i] = A[base + i];
        contexts[1][i] = B[base + i];
    }

    contexts[0][EXEC_STATUS_WORD] &= 0x0000FFFFFFFFFFFFUL;

    while (true) {
        ulong pc = contexts[0][REG_PC];
        if (pc >= MAX_PC) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_EXHAUSTED);
            break;
        }

        ulong wordPos = PROGRAM_INDEX_WORD + (pc / 2);
        if (wordPos >= WORDS) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_BAD_WORD);
            break;
        }
        uint shift = (pc % 2) * 32;
        uint instr = (uint)(contexts[0][wordPos] >> shift);

        uchar op = instr & 0xF;
        if (op == 0 && pc > 0) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_HALT);
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
            uint srcIdx = srcCode & 0x0FFF;
            if (srcIdx < WORDS) {
                srcVal = contexts[0][srcIdx];
            } else {
                srcVal = 0;
            }
        } else {
            srcVal = (ulong)srcCode;
        }

        ulong dstVal = 0;
        bool dSpan = false;
        if ((dstCode & 0x1000) != 0) {
            dstVal = (ulong)(dstCode & 0x0FFF);
            dSpan = true;
        } else if ((dstCode & 0x2000) != 0) {
            uint dstIdxReg = dstCode & 0x0FFF;
            if (dstIdxReg < WORDS) {
                dstVal = contexts[0][dstIdxReg];
            } else {
                dstVal = 0;
            }
        } else {
            dstVal = (ulong)dstCode;
        }

        if (sSpan || dSpan) {
            ulong sBase = srcVal;
            ulong sCtx = 0, sOff = 0, sLen = 0;
            if (sBase + 2 < WORDS) {
                sCtx = contexts[0][sBase];
                sOff = contexts[0][sBase+1];
                sLen = contexts[0][sBase+2];
            }

            ulong dBase = dstVal;
            ulong dCtx = 0, dOff = 0, dLen = 0;
            if (dBase + 2 < WORDS) {
                dCtx = contexts[0][dBase];
                dOff = contexts[0][dBase+1];
                dLen = contexts[0][dBase+2];
            }

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
        if (dstIdx < WORDS) {
            contexts[0][dstIdx] = res;
        }
    }

    for (int i = 0; i < WORDS; i++) {
        A[base + i] = contexts[0][i];
    }
}

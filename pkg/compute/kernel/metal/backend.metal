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

    const ulong MAX_SPAN = 1048576UL;
    ulong contexts[2][WORDS];
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
        if (instr == 0) {
            MARK_EXEC_EXIT(contexts[0], EXEC_EXIT_HALT);
            break;
        }

        uint16_t sc = (instr >> 4) & 0x3FFF;
        uint16_t dc = (instr >> 18) & 0x3FFF;
        bool sSp = (sc & 0x3F80) == 0x3000;
        bool dSp = (dc & 0x3F80) == 0x3000;

        contexts[0][REG_PC]++;

        if (sSp && dSp) {
            ulong sB = (ulong)(sc & 0x7F);
            ulong dB = (ulong)(dc & 0x7F);
            if (sB + 2 >= WORDS || dB + 2 >= WORDS) {
                continue;
            }

            ulong sL = contexts[0][sB] & 1UL;
            ulong sS = contexts[0][sB+1];
            ulong sE = contexts[0][sB+2];
            ulong dL = contexts[0][dB] & 1UL;
            ulong dS = contexts[0][dB+1];
            ulong dE = contexts[0][dB+2];
            if (sE <= sS || dE <= dS) {
                continue;
            }

            ulong sN = sE - sS;
            ulong dN = dE - dS;
            ulong limit = (sN < dN) ? sN : dN;
            if (sN == 1) {
                limit = dN;
            }
            if (limit > MAX_SPAN) {
                limit = MAX_SPAN;
            }
            for (ulong i = 0; i < limit; i++) {
                ulong sBit = sS + i;
                if (sN == 1) {
                    sBit = sS;
                }
                ulong sWord = sBit / 64;
                ulong sShift = sBit % 64;
                ulong dWord = (dS + i) / 64;
                ulong dShift = (dS + i) % 64;
                if (sWord >= WORDS || dWord >= WORDS) {
                    break;
                }

                uchar sb = (contexts[sL][sWord] >> sShift) & 1;
                uchar db = (contexts[dL][dWord] >> dShift) & 1;

                uint idx = (1 - db) | ((1 - sb) << 1);
                uchar res = (op >> idx) & 1;

                if (res == 1) {
                    contexts[dL][dWord] |= (1ULL << dShift);
                } else {
                    contexts[dL][dWord] &= ~(1ULL << dShift);
                }
            }
            continue;
        }

        if (dSp) {
            uint dstIdx = (uint)(dc & 0x7F);
            if (dstIdx >= WORDS) {
                continue;
            }

            ulong left = (ulong)sc;
            if ((sc & 0x3F80) == 0x3000) {
                uint srcIdx = (uint)(sc & 0x7F);
                if (srcIdx >= WORDS) {
                    continue;
                }
                left = contexts[0][srcIdx];
            }
            ulong right = contexts[0][dstIdx];

            ulong m0 = 0 - (ulong)((op >> 3) & 1);
            ulong m1 = 0 - (ulong)((op >> 2) & 1);
            ulong m2 = 0 - (ulong)((op >> 1) & 1);
            ulong m3 = 0 - (ulong)(op & 1);

            ulong res = m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right));
            contexts[0][dstIdx] = res;
        }
    }

    for (int i = 0; i < WORDS; i++) {
        A[base + i] = contexts[0][i];
    }
}

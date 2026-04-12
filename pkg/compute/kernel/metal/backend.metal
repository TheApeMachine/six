#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

#define CONTEXT_START_WORD 32
#define GRADIENT_START_WORD 40
#define OPCODE_GEOMETRIC_MASK 0xF0
#define OPCODE_GEOMETRIC_COMPOSE 0x10
#define OPCODE_GEOMETRIC_SANDWICH 0x20
#define OPCODE_GEOMETRIC_REVERSE 0x30
#define OPCODE_REGION_PROGRAM 0x40
#define RESERVED_START_WORD 56

struct Multivector {
    float v[8];
};

static inline float double_word_to_float(ulong word) {
    uint sign = (uint)(word >> 63);
    uint exponent = (uint)((word >> 52) & 0x7FF);
    ulong mantissa = word & 0x000FFFFFFFFFFFFFUL;

    if (exponent == 0 && mantissa == 0) {
        return as_type<float>(sign << 31);
    }

    if (exponent == 0) {
        return as_type<float>(sign << 31);
    }

    if (exponent == 0x7FF) {
        uint nan = mantissa == 0 ? 0u : 0x00400000u;
        return as_type<float>((sign << 31) | 0x7F800000u | nan);
    }

    int floatExponent = int(exponent) - 1023 + 127;

    if (floatExponent <= 0) {
        return as_type<float>(sign << 31);
    }

    if (floatExponent >= 0xFF) {
        return as_type<float>((sign << 31) | 0x7F800000u);
    }

    ulong rounded = mantissa >> 29;
    ulong remainder = mantissa & 0x000000001FFFFFFFUL;
    ulong halfway = 0x0000000010000000UL;

    if (remainder > halfway || (remainder == halfway && (rounded & 1UL) != 0)) {
        rounded++;
    }

    if (rounded == 0x00800000UL) {
        floatExponent++;
        rounded = 0;
    }

    if (floatExponent >= 0xFF) {
        return as_type<float>((sign << 31) | 0x7F800000u);
    }

    uint floatMantissa = (uint)(rounded & 0x007FFFFFUL);

    return as_type<float>((sign << 31) | (uint(floatExponent) << 23) | floatMantissa);
}

static inline ulong float_to_double_word(float value) {
    uint bits = as_type<uint>(value);
    uint sign = bits >> 31;
    uint exponent = (bits >> 23) & 0xFF;
    uint mantissa = bits & 0x007FFFFF;

    if (exponent == 0 && mantissa == 0) {
        return ulong(sign) << 63;
    }

    if (exponent == 0xFF) {
        ulong nan = mantissa == 0 ? 0UL : 0x0008000000000000UL;
        return (ulong(sign) << 63) | (0x7FFUL << 52) | nan;
    }

    if (exponent == 0) {
        int doubleExponent = -126 + 1023;
        uint normalized = mantissa;

        while ((normalized & 0x00800000u) == 0) {
            normalized <<= 1;
            doubleExponent--;
        }

        normalized &= 0x007FFFFF;

        return (ulong(sign) << 63) |
               (ulong(doubleExponent) << 52) |
               (ulong(normalized) << 29);
    }

    int doubleExponent = int(exponent) - 127 + 1023;

    return (ulong(sign) << 63) |
           (ulong(doubleExponent) << 52) |
           (ulong(mantissa) << 29);
}

static inline Multivector load_multivector(device ulong* frame, uint start) {
    Multivector mv;
    for (int idx = 0; idx < 8; idx++) {
        mv.v[idx] = double_word_to_float(frame[start + idx]);
    }
    return mv;
}

static inline void store_multivector(device ulong* frame, uint start, Multivector mv) {
    for (int idx = 0; idx < 8; idx++) {
        frame[start + idx] = float_to_double_word(mv.v[idx]);
    }
}

static inline Multivector geometric_product(Multivector left, Multivector right) {
    Multivector out;
    out.v[0] = left.v[0]*right.v[0] - left.v[4]*right.v[4] - left.v[5]*right.v[5] - left.v[6]*right.v[6];
    out.v[1] = left.v[0]*right.v[1] + left.v[1]*right.v[0] - left.v[2]*right.v[4] + left.v[3]*right.v[5] +
               left.v[4]*right.v[2] - left.v[5]*right.v[3] - left.v[6]*right.v[7] - left.v[7]*right.v[6];
    out.v[2] = left.v[0]*right.v[2] + left.v[1]*right.v[4] + left.v[2]*right.v[0] - left.v[3]*right.v[6] -
               left.v[4]*right.v[1] - left.v[5]*right.v[7] + left.v[6]*right.v[3] - left.v[7]*right.v[5];
    out.v[3] = left.v[0]*right.v[3] - left.v[1]*right.v[5] + left.v[2]*right.v[6] + left.v[3]*right.v[0] -
               left.v[4]*right.v[7] + left.v[5]*right.v[1] - left.v[6]*right.v[2] - left.v[7]*right.v[4];
    out.v[4] = left.v[0]*right.v[4] + left.v[4]*right.v[0] + left.v[5]*right.v[6] - left.v[6]*right.v[5];
    out.v[5] = left.v[0]*right.v[5] - left.v[4]*right.v[6] + left.v[5]*right.v[0] + left.v[6]*right.v[4];
    out.v[6] = left.v[0]*right.v[6] + left.v[4]*right.v[5] - left.v[5]*right.v[4] + left.v[6]*right.v[0];
    out.v[7] = left.v[0]*right.v[7] + left.v[1]*right.v[6] + left.v[2]*right.v[5] + left.v[3]*right.v[4] +
               left.v[4]*right.v[3] + left.v[5]*right.v[2] + left.v[6]*right.v[1] + left.v[7]*right.v[0];
    return out;
}

static inline Multivector reverse(Multivector mv) {
    Multivector out;
    out.v[0] =  mv.v[0];
    out.v[1] = -mv.v[1];
    out.v[2] = -mv.v[2];
    out.v[3] = -mv.v[3];
    out.v[4] = -mv.v[4];
    out.v[5] = -mv.v[5];
    out.v[6] = -mv.v[6];
    out.v[7] =  mv.v[7];
    return out;
}

static inline Multivector sandwich(Multivector motor, Multivector target) {
    return geometric_product(geometric_product(motor, target), reverse(motor));
}

static inline void unpack_region_ref(ulong word, thread int* start, thread int* span) {
    *start = (int)(uint)word;
    *span = (int)(uint)(word >> 32);
}

static inline void universal_bitwise_v2_device(
    device ulong* frame,
    int aStart, int aSpan,
    int bStart, int bSpan,
    int dstStart, int dstSpan,
    int mode, ulong opcodeTable
) {
    if (aSpan <= 0 || bSpan <= 0 || dstSpan <= 0) return;
    if (aStart < 0 || bStart < 0 || dstStart < 0) return;
    if (aStart + aSpan > 128 || bStart + bSpan > 128 || dstStart + dstSpan > 128) return;

    ulong aLane[4] = {0, 0, 0, 0};
    for (int idx = 0; idx < aSpan; idx++) {
        aLane[idx & 3] ^= frame[aStart + idx];
    }

    uchar sigBytes[64];
    for (int i = 0; i < 64; i++) sigBytes[i] = 0;

    for (int rot = 0; rot < 16; rot++) {
        uchar op = (uchar)((opcodeTable >> (rot * 4)) & 0xF);
        ulong m0 = (op & 1) ? ~0UL : 0UL;
        ulong m1 = (op & 2) ? ~0UL : 0UL;
        ulong m2 = (op & 4) ? ~0UL : 0UL;
        ulong m3 = (op & 8) ? ~0UL : 0UL;

        for (int lane = 0; lane < 4; lane++) {
            int bIdx = bStart + ((rot * 4) + lane) % bSpan;
            ulong a = aLane[lane];
            ulong b = frame[bIdx];
            ulong notA = ~a;
            ulong notB = ~b;

            ulong result = (a & b & m0) |
                           (a & notB & m1) |
                           (notA & b & m2) |
                           (notA & notB & m3);

            sigBytes[rot * 4 + lane] = (uchar)(result & 0xFF);
        }
    }

    ulong sigWords[8];
    for (int w = 0; w < 8; w++) {
        int base = w * 8;
        sigWords[w] = (ulong)sigBytes[base] |
                      ((ulong)sigBytes[base + 1] << 8) |
                      ((ulong)sigBytes[base + 2] << 16) |
                      ((ulong)sigBytes[base + 3] << 24) |
                      ((ulong)sigBytes[base + 4] << 32) |
                      ((ulong)sigBytes[base + 5] << 40) |
                      ((ulong)sigBytes[base + 6] << 48) |
                      ((ulong)sigBytes[base + 7] << 56);
    }

    if (mode == 0) {
        int limit = dstSpan;
        if (limit > 8) limit = 8;
        for (int idx = 0; idx < limit; idx++) {
            frame[dstStart + idx] ^= sigWords[idx];
        }
        return;
    }

    ulong total = 0;
    for (int idx = 0; idx < 8; idx++) {
        total += popcount(sigWords[idx]);
    }
    frame[dstStart] = total;
}

kernel void unified_bitwise_kernel(
    device ulong* A [[buffer(0)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    device ulong* frame = A + base;

    uchar rawOpcode = (uchar)(frame[PROGRAM_START_WORD] & 0xFF);

    if (rawOpcode == OPCODE_REGION_PROGRAM) {
        for (int offset = 0; offset < 60; offset += 6) {
            ulong op = frame[RESERVED_START_WORD + offset];
            if (op == 0 && offset > 0) break;

            ulong rotationTable = frame[RESERVED_START_WORD + offset + 1];
            if (rotationTable == 0) continue;

            int mode = (int)(frame[RESERVED_START_WORD + offset + 2] & 0xFF);
            int aStart, aSpan, bStart, bSpan, dstStart, dstSpan;
            unpack_region_ref(frame[RESERVED_START_WORD + offset + 3], &aStart, &aSpan);
            unpack_region_ref(frame[RESERVED_START_WORD + offset + 4], &bStart, &bSpan);
            unpack_region_ref(frame[RESERVED_START_WORD + offset + 5], &dstStart, &dstSpan);

            universal_bitwise_v2_device(frame, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, mode, rotationTable);
        }
        return;
    }

    ulong rotationTable = frame[PROGRAM_START_WORD + 1];
    if (rotationTable == 0) return;

    int mode = (int)(frame[PROGRAM_START_WORD + 2] & 0xFF);
    int aStart, aSpan, bStart, bSpan, dstStart, dstSpan;
    unpack_region_ref(frame[PROGRAM_START_WORD + 3], &aStart, &aSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 4], &bStart, &bSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 5], &dstStart, &dstSpan);

    universal_bitwise_v2_device(frame, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, mode, rotationTable);
}

kernel void nearest_affinity_kernel(
    device const ulong* candidates [[buffer(0)]],
    device const ulong* query      [[buffer(1)]],
    device uint*        distances  [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * AFFINITY_WORDS;
    uint dist = 0;

    for (int w = 0; w < AFFINITY_WORDS; w++) {
        dist += popcount(candidates[base + w] ^ query[w]);
    }

    distances[id] = dist;
}

kernel void geometric_kernel(
    device ulong* A [[buffer(0)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    device ulong* frame = A + base;
    uchar op = (uchar)(frame[PROGRAM_START_WORD] & OPCODE_GEOMETRIC_MASK);

    if (op != OPCODE_GEOMETRIC_COMPOSE &&
        op != OPCODE_GEOMETRIC_SANDWICH &&
        op != OPCODE_GEOMETRIC_REVERSE) {
        return;
    }

    Multivector left = load_multivector(frame, CONTEXT_START_WORD);
    Multivector right = load_multivector(frame, GRADIENT_START_WORD);

    if (op == OPCODE_GEOMETRIC_COMPOSE) {
        store_multivector(frame, SIGNALS_START_WORD, geometric_product(left, right));
        return;
    }

    if (op == OPCODE_GEOMETRIC_SANDWICH) {
        store_multivector(frame, SIGNALS_START_WORD, sandwich(left, right));
        return;
    }

    if (op == OPCODE_GEOMETRIC_REVERSE) {
        store_multivector(frame, SIGNALS_START_WORD, reverse(left));
        return;
    }
}

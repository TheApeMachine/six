#include <metal_stdlib>
#include "../shared/primitives.h"
#include "../shared/postexec_layout.h"
#include "device_postexec.metal"
using namespace metal;

#define CONTEXT_START_WORD 32
#define GRADIENT_START_WORD 40
#define OPCODE_GEOMETRIC_MASK 0xF0
#define OPCODE_GEOMETRIC_COMPOSE 0x10
#define OPCODE_GEOMETRIC_SANDWICH 0x20
#define OPCODE_GEOMETRIC_REVERSE 0x30
#define OPCODE_REGION_PROGRAM 0x40
#define OPCODE_COPY_MASK_MERGE 0x50
#define RESERVED_START_WORD 56
#define PROGRAM_CONTRACT_SHIFT 8
#define CONTRACT_EXACT_BINARY 1

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

static inline ulong exact_binary_word(ulong op, ulong a, ulong b) {
    ulong m0 = (op & 1) ? ~0UL : 0UL;
    ulong m1 = (op & 2) ? ~0UL : 0UL;
    ulong m2 = (op & 4) ? ~0UL : 0UL;
    ulong m3 = (op & 8) ? ~0UL : 0UL;
    return (a & b & m0) |
           (a & ~b & m1) |
           (~a & b & m2) |
           (~a & ~b & m3);
}

static inline void copy_mask_merge_device(device ulong* frame) {
    int aStart, aSpan, bStart, bSpan, dstStart, dstSpan;
    unpack_region_ref(frame[PROGRAM_START_WORD + 3], &aStart, &aSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 4], &bStart, &bSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 5], &dstStart, &dstSpan);
    int n = aSpan;
    if (bSpan < n) n = bSpan;
    if (dstSpan < n) n = dstSpan;
    if (n <= 0) return;
    if (aStart < 0 || bStart < 0 || dstStart < 0) return;
    if (aStart + n > 128 || bStart + n > 128 || dstStart + n > 128) return;
    for (int idx = 0; idx < n; idx++) {
        ulong mask = frame[bStart + idx];
        ulong src = frame[aStart + idx];
        int dst = dstStart + idx;
        frame[dst] = (src & mask) | (frame[dst] & ~mask);
    }
}

static inline void exact_binary_device(
    device ulong* frame,
    ulong op,
    int aStart, int aSpan,
    int bStart, int bSpan,
    int dstStart, int dstSpan
) {
    if (aSpan <= 0 || bSpan <= 0 || dstSpan <= 0) return;
    if (aStart < 0 || bStart < 0 || dstStart < 0) return;
    int limit = aSpan;
    if (bSpan < limit) limit = bSpan;
    if (dstSpan < limit) limit = dstSpan;
    if (aStart + limit > 128 || bStart + limit > 128 || dstStart + limit > 128) return;
    for (int idx = 0; idx < limit; idx++) {
        frame[dstStart + idx] = exact_binary_word(op, frame[aStart + idx], frame[bStart + idx]);
    }
}

static inline void universal_bitwise_device(
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

static inline ulong broadcast_opcode_nibble(uchar opLow) {
    ulong n = (ulong)(opLow & 0xF);
    ulong packed = 0;
    for (int i = 0; i < 16; i++) {
        packed |= n << (i * 4);
    }
    return packed;
}

static inline void emit_clone_device_metal(
    device ulong* arena,
    device ulong* parent_frame,
    uint parent_slot,
    device atomic_uint* linear_next,
    device uint* spawn_parent,
    device uint* spawn_child,
    device atomic_uint* spawn_tail,
    uint max_slots
) {
    uint child_slot = atomic_fetch_add_explicit(linear_next, 1u, memory_order_relaxed);
    if (child_slot >= max_slots) {
        parent_frame[SIGNALS_START_WORD] = ~0UL;
        return;
    }
    device ulong* child = arena + (uint64_t)child_slot * (uint64_t)WORDS;
    for (int i = 0; i < WORDS; i++) {
        child[i] = parent_frame[i];
    }
    ulong noise = parent_frame[PROPERTIES_NOISE_WORD];
    for (int w = 0; w < AFFINITY_WORDS; w++) {
        child[AFFINITY_START_WORD + w] ^= noise ^ ((ulong)parent_slot << (ulong)(w * 13));
    }
    uint tail = atomic_fetch_add_explicit(spawn_tail, 1u, memory_order_relaxed);
    uint pos = tail % SPAWN_QUEUE_CAP;
    spawn_parent[pos] = parent_slot;
    spawn_child[pos] = child_slot;
}

kernel void unified_bitwise_kernel(
    device ulong* A [[buffer(0)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    device ulong* frame = A + base;

    uchar opLow = (uchar)(frame[PROGRAM_START_WORD] & 0xF);
    if (opLow == 0) {
        finish_frame_post_alu_device(frame);
        return;
    }

    ulong opcodeTable = broadcast_opcode_nibble(opLow);
    int mode = (int)(frame[PROGRAM_START_WORD + 1] & 0xFF);
    int aStart, aSpan, bStart, bSpan, dstStart, dstSpan;
    unpack_region_ref(frame[PROGRAM_START_WORD + 3], &aStart, &aSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 4], &bStart, &bSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 5], &dstStart, &dstSpan);

    universal_bitwise_device(frame, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, mode, opcodeTable);
    finish_frame_post_alu_device(frame);
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
        finish_frame_post_alu_device(frame);
        return;
    }

    if (op == OPCODE_GEOMETRIC_SANDWICH) {
        store_multivector(frame, SIGNALS_START_WORD, sandwich(left, right));
        finish_frame_post_alu_device(frame);
        return;
    }

    if (op == OPCODE_GEOMETRIC_REVERSE) {
        store_multivector(frame, SIGNALS_START_WORD, reverse(left));
        finish_frame_post_alu_device(frame);
        return;
    }
}

kernel void geometric_arena_indices_kernel(
    device ulong* arena [[buffer(0)]],
    device const uint* indices [[buffer(1)]],
    uint id [[thread_position_in_grid]]
) {
    uint slot = indices[id];
    device ulong* frame = arena + (uint64_t)slot * (uint64_t)WORDS;
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
        finish_frame_post_alu_device(frame);
        return;
    }

    if (op == OPCODE_GEOMETRIC_SANDWICH) {
        store_multivector(frame, SIGNALS_START_WORD, sandwich(left, right));
        finish_frame_post_alu_device(frame);
        return;
    }

    if (op == OPCODE_GEOMETRIC_REVERSE) {
        store_multivector(frame, SIGNALS_START_WORD, reverse(left));
        finish_frame_post_alu_device(frame);
        return;
    }
}

kernel void unified_bitwise_arena_indices_kernel(
    device ulong* arena [[buffer(0)]],
    device const uint* indices [[buffer(1)]],
    device atomic_uint* linear_next [[buffer(2)]],
    device uint* spawn_parent [[buffer(3)]],
    device uint* spawn_child [[buffer(4)]],
    device atomic_uint* spawn_tail [[buffer(5)]],
    device const uint* max_slots_buf [[buffer(6)]],
    uint id [[thread_position_in_grid]]
) {
    uint max_slots = max_slots_buf[0];
    uint parent_slot = indices[id];
    device ulong* frame = arena + (uint64_t)parent_slot * (uint64_t)WORDS;

    uchar rawOpcode = (uchar)(frame[PROGRAM_START_WORD] & 0xFF);

    if (rawOpcode == OPCODE_EMIT_CLONE) {
        emit_clone_device_metal(
            arena,
            frame,
            parent_slot,
            linear_next,
            spawn_parent,
            spawn_child,
            spawn_tail,
            max_slots
        );
        finish_frame_post_alu_device(frame);
        return;
    }

    uchar opLow = (uchar)(frame[PROGRAM_START_WORD] & 0xF);
    if (opLow == 0) {
        finish_frame_post_alu_device(frame);
        return;
    }

    ulong opcodeTable = broadcast_opcode_nibble(opLow);
    int mode = (int)(frame[PROGRAM_START_WORD + 1] & 0xFF);
    int aStart, aSpan, bStart, bSpan, dstStart, dstSpan;
    unpack_region_ref(frame[PROGRAM_START_WORD + 3], &aStart, &aSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 4], &bStart, &bSpan);
    unpack_region_ref(frame[PROGRAM_START_WORD + 5], &dstStart, &dstSpan);

    universal_bitwise_device(frame, aStart, aSpan, bStart, bSpan, dstStart, dstSpan, mode, opcodeTable);
    finish_frame_post_alu_device(frame);
}

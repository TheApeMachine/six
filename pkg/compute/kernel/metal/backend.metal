#include <metal_stdlib>
#include "../shared/primitives.h"
using namespace metal;

#define CONTEXT_START_WORD 32
#define GRADIENT_START_WORD 40
#define OPCODE_GEOMETRIC_MASK 0xF0
#define OPCODE_GEOMETRIC_COMPOSE 0x10
#define OPCODE_GEOMETRIC_SANDWICH 0x20
#define OPCODE_GEOMETRIC_REVERSE 0x30

/*
Multivector is the Cl(3,0,1) 8-lane payload carried in the Value frame.
The ABI remains uint64 because the same 1024-byte frame is also consumed by
the Boolean ALU; the Metal kernel decodes those lanes at the arithmetic
boundary because MSL has no native double type.
*/
struct Multivector {
    float v[8];
};

/*
Apple's Metal Shading Language does not expose double on the GPU. The Metal
path therefore keeps the 64-bit frame ABI and performs explicit IEEE-754
boundary conversion into native float arithmetic.
*/
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

    // Word 17 (prog[1]) packs 16 × 4-bit tile opcodes little-endian. Legacy
    // single-opcode callers broadcast the same nibble 16× so the per-rotation
    // decode collapses to the old broadcast semantics. Per-rotation programs
    // (e.g. Coupling, which splits AND and OR across the sweep) place
    // distinct nibbles so each rotation pulls its own truth table.
    ulong tileWord = prog[1];

    for (int rot = 0; rot < NUM_ROTATIONS; rot++) {
        uchar op = (uchar)((tileWord >> (rot * 4)) & 0xF);

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

/*
NearestAffinity kernel — parallel Hamming distance search.

One thread per candidate. Each thread computes the Hamming distance
between the query vector (buffer 1) and its candidate vector
(buffer 0, stride AFFINITY_WORDS), then writes the distance to an
output buffer. The host reduces the argmin from the distance array.
*/
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

/*
Geometric kernel — PGA lane for Value-local multivectors.

The opcode is read from the high nibble of Program[0], leaving the low nibble
available for Boolean truth tables. Context and Gradient remain 8x 64-bit
frame lanes while the Metal arithmetic core runs the converted values as
native floats.
*/
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

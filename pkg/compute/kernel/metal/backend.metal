#include <metal_stdlib>
using namespace metal;

/*
    SIX NATIVE VM KERNELS (APPLE SILICON MSL)
    
    All kernels operate on `primitive.Value` arrays: 128 ulong words (8192 bits).
    The first 8191 bits map to the GF(8191) Mersenne Prime Field.
    The 8192nd bit (bit 63 of word 127) is the OOB Instruction Mask.
*/

constant uint  WORDS     = 128;
constant uint  CORE_BITS = 8191;
constant ulong LAST_MASK = (1UL << 63) - 1; // 0x7FFFFFFFFFFFFFFF

/*
    GF(8191) Mersenne Prime Math
*/

// Fast branchless reduction for the 8191 Mersenne Prime
inline uint mod8191(uint x) {
    x = (x >> 13) + (x & 8191);
    if (x >= 8191) x -= 8191;
    return x;
}

// Extract (Scale, Translate) Affine Motor from a Value
inline uint2 derive_motor(device const ulong* val) {
    uint s = 1;
    uint t = 0;
    
    for (uint i = 0; i < WORDS; i++) {
        ulong w = val[i];
        if (i == WORDS - 1) w &= LAST_MASK;
        
        while (w != 0) {
            uint bit = ctz(w);
            uint p = i * 64 + bit;
            s = mod8191(s * p);
            t = mod8191(t + p);
            w &= w - 1UL;
        }
    }
    
    if (s == 0) s = 1;
    return uint2(s, t);
}

// Extended Euclidean Algorithm for GF(8191) inverse
inline uint2 invert_motor(uint scale, uint translate) {
    int t = 0, newT = 1;
    int r = 8191, newR = int(scale);

    while (newR != 0) {
        int quotient = r / newR;

        int tempT = t - quotient * newT;
        t = newT;
        newT = tempT;

        int tempR = r - quotient * newR;
        r = newR;
        newR = tempR;
    }

    if (t < 0) t += 8191;
    
    uint invScale = uint(t);
    uint invTranslate = (8191 - (invScale * translate) % 8191) % 8191;

    return uint2(invScale, invTranslate);
}


/*
    1. VECTORIZED BITWISE OPERATIONS
    Using ulong4 vectors (256-bit registers) to process an entire 1024-byte Value
    in exactly 32 loops per thread.
*/

#define BITWISE_KERNEL(name, EXPR) \
kernel void bitwise_##name( \
    device const ulong4* A [[buffer(0)]], \
    device const ulong4* B [[buffer(1)]], \
    device ulong4* dst [[buffer(2)]], \
    uint id [[thread_position_in_grid]] \
) { \
    uint base = id * 32; \
    for (uint i = 0; i < 32; i++) { \
        ulong4 a = A[base + i]; \
        ulong4 b = B[base + i]; \
        dst[base + i] = EXPR; \
    } \
    dst[base + 31].w &= LAST_MASK; \
}

// Generate the 8 Binary Operations
BITWISE_KERNEL(or,   a | b)
BITWISE_KERNEL(and,  a & b)
BITWISE_KERNEL(xor,  a ^ b)
BITWISE_KERNEL(and_not, a & ~b)
BITWISE_KERNEL(nand, ~(a & b))
BITWISE_KERNEL(nor,  ~(a | b))
BITWISE_KERNEL(xnor, ~(a ^ b))
BITWISE_KERNEL(converse_nonimplication, b & ~a)

// Unary NOT Operation
kernel void bitwise_not(
    device const ulong4* A [[buffer(0)]],
    device ulong4* dst [[buffer(1)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * 32;
    for (uint i = 0; i < 32; i++) {
        dst[base + i] = ~A[base + i];
    }
    dst[base + 31].w &= LAST_MASK;
}

/*
    2. AFFINE MOTOR OPERATIONS
*/

kernel void motor_apply(
    device const ulong* A [[buffer(0)]],
    device const ulong* B [[buffer(1)]],
    device ulong* dst [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    uint2 st = derive_motor(A + base);
    
    ulong tmp[WORDS] = {0};

    for (uint i = 0; i < WORDS; i++) {
        ulong w = B[base + i];
        if (i == WORDS - 1) w &= LAST_MASK;

        while (w != 0) {
            uint bit = ctz(w);
            uint p = i * 64 + bit;
            uint mapped = mod8191(st.x * p + st.y);
            tmp[mapped / 64] |= (1UL << (mapped % 64));
            w &= w - 1UL;
        }
    }

    for (uint i = 0; i < WORDS; i++) {
        dst[base + i] = tmp[i];
    }
}

kernel void motor_invert(
    device const ulong* A [[buffer(0)]],
    device const ulong* B [[buffer(1)]],
    device ulong* dst [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    uint2 st = derive_motor(A + base);
    uint2 inv_st = invert_motor(st.x, st.y);
    
    ulong tmp[WORDS] = {0};

    for (uint i = 0; i < WORDS; i++) {
        ulong w = B[base + i];
        if (i == WORDS - 1) w &= LAST_MASK;

        while (w != 0) {
            uint bit = ctz(w);
            uint p = i * 64 + bit;
            uint mapped = mod8191(inv_st.x * p + inv_st.y);
            tmp[mapped / 64] |= (1UL << (mapped % 64));
            w &= w - 1UL;
        }
    }

    for (uint i = 0; i < WORDS; i++) {
        dst[base + i] = tmp[i];
    }
}

kernel void motor_compose(
    device const ulong* A [[buffer(0)]],
    device const ulong* B [[buffer(1)]],
    device ulong* dst [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    uint2 stA = derive_motor(A + base);
    uint2 stB = derive_motor(B + base);

    // f2(f1(p)) = s2*(s1*p + t1) + t2
    uint comp_s = mod8191(stB.x * stA.x);
    uint comp_t = mod8191(stB.x * stA.y + stB.y);

    ulong tmp[WORDS] = {0};

    for (uint i = 0; i < WORDS; i++) {
        ulong w = B[base + i];
        if (i == WORDS - 1) w &= LAST_MASK;

        while (w != 0) {
            uint bit = ctz(w);
            uint p = i * 64 + bit;
            uint mapped = mod8191(comp_s * p + comp_t);
            tmp[mapped / 64] |= (1UL << (mapped % 64));
            w &= w - 1UL;
        }
    }

    for (uint i = 0; i < WORDS; i++) {
        dst[base + i] = tmp[i];
    }
}


/*
    3. STRUCTURAL GEOMETRY
*/

kernel void roll_left(
    device const ulong* src [[buffer(0)]],
    device ulong* dst [[buffer(1)]],
    constant uint& shift_amount [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * WORDS;
    uint s = ((shift_amount % CORE_BITS) + CORE_BITS) % CORE_BITS;
    
    if (s == 0) {
        for (uint i = 0; i < WORDS; i++) dst[base + i] = src[base + i];
        return;
    }

    uint r = CORE_BITS - s;
    uint wShiftL = s / 64;
    uint bShiftL = s % 64;
    uint wShiftR = r / 64;
    uint bShiftR = r % 64;

    ulong tmp[WORDS] = {0};

    // 1. Left shift limits
    if (bShiftL == 0) {
        for(uint i = wShiftL; i < WORDS; i++) tmp[i] = src[base + i - wShiftL];
    } else {
        tmp[wShiftL] = src[base + 0] << bShiftL;
        for(uint i = wShiftL + 1; i < WORDS; i++) {
            tmp[i] = (src[base + i - wShiftL] << bShiftL) | (src[base + i - wShiftL - 1] >> (64 - bShiftL));
        }
    }

    // 2. Right shift wraparound
    if (bShiftR == 0) {
        for(uint i = 0; i < WORDS - wShiftR; i++) tmp[i] |= src[base + i + wShiftR];
    } else {
        for(uint i = 0; i < WORDS - wShiftR - 1; i++) {
            tmp[i] |= (src[base + i + wShiftR] >> bShiftR) | (src[base + i + wShiftR + 1] << (64 - bShiftR));
        }
        tmp[WORDS - 1 - wShiftR] |= src[base + WORDS - 1] >> bShiftR;
    }

    tmp[WORDS - 1] &= LAST_MASK;

    for (uint i = 0; i < WORDS; i++) {
        dst[base + i] = tmp[i];
    }
}
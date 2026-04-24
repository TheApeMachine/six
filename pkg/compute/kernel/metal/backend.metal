#include <metal_stdlib>
#include "../shared/primitives.h"
#include "../shared/postexec_layout.h"
#include "device_postexec.metal"
using namespace metal;

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

// Padded affinity-row stride matches kernel/cpu/affinity_distances.go's
// affinityDistanceVectorWords (8 × uint64). Keep in lock-step with the CPU
// reference and the CUDA kernel so the host can ship one packing buffer
// across all backends.
#define AFFINITY_ROW_WORDS 8

struct BatchFirstFitParams {
    uint community_count;
    uint value_count;
    uint hamming_budget;
    uint saturation_cap;
};

kernel void batch_first_fit_kernel(
    device const ulong* community_ors [[buffer(0)]],
    device const ulong* value_affinities [[buffer(1)]],
    constant BatchFirstFitParams& params [[buffer(2)]],
    device int* out [[buffer(3)]],
    uint vid [[thread_position_in_grid]]
) {
    if (vid >= params.value_count) return;

    // Hold the value's 257 bits in thread-private registers across the
    // entire community sweep — the kernel never reloads them.
    uint v_base = vid * AFFINITY_ROW_WORDS;
    ulong v0 = value_affinities[v_base + 0];
    ulong v1 = value_affinities[v_base + 1];
    ulong v2 = value_affinities[v_base + 2];
    ulong v3 = value_affinities[v_base + 3];
    ulong v4 = value_affinities[v_base + 4] & 1UL;

    uint hbud = params.hamming_budget;
    uint scap = params.saturation_cap;

    int hit = -1;

    for (uint c = 0; c < params.community_count; c++) {
        uint c_base = c * AFFINITY_ROW_WORDS;
        ulong c0 = community_ors[c_base + 0];
        ulong c1 = community_ors[c_base + 1];
        ulong c2 = community_ors[c_base + 2];
        ulong c3 = community_ors[c_base + 3];
        ulong c4 = community_ors[c_base + 4] & 1UL;

        uint hamming =
            (uint)popcount(v0 ^ c0) +
            (uint)popcount(v1 ^ c1) +
            (uint)popcount(v2 ^ c2) +
            (uint)popcount(v3 ^ c3) +
            (uint)(v4 ^ c4);

        if (hamming > hbud) continue;

        uint unionc =
            (uint)popcount(v0 | c0) +
            (uint)popcount(v1 | c1) +
            (uint)popcount(v2 | c2) +
            (uint)popcount(v3 | c3) +
            (uint)(v4 | c4);

        if (unionc > scap) continue;

        hit = (int)c;
        break;
    }

    out[vid] = hit;
}

kernel void nearest_affinity_kernel(
    device const ulong* candidates [[buffer(0)]],
    device const ulong* query      [[buffer(1)]],
    device atomic_ulong* best_packed_result [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    uint base = id * AFFINITY_WORDS;
    uint dist_sq = 0;

    for (int w = 0; w < AFFINITY_WORDS; w++) {
        dist_sq += popcount(candidates[base + w] ^ query[w]);
    }

    // We want to find the MINIMUM distance.
    // atomic_max requires us to pack score such that MAX value is best.
    // So we invert the distance. Max dist_sq is 131072.
    // Let's pack: (131072 - dist_sq) in the upper 32 bits, and global_id in the lower 32.
    
    uint32_t dist_u32 = (uint32_t)dist_sq;
    uint32_t inverted_dist = 131072 - dist_u32; 

    uint global_id = id;

    uint64_t packed_result = ((uint64_t)inverted_dist << 32) | (uint64_t)global_id;

    atomic_max_explicit(
        best_packed_result,
        (ulong)packed_result,
        memory_order_relaxed
    );
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

/*
hypercube_gossip_kernel diffuses each Value's S+C+G+P band across the
community using an XOR-routed hypercube. Threadgroup memory plays the
role of the on-chip crossbar; for a full 256-Value field, dimensions
0..4 use simd_shuffle_xor (in-warp, register) and 5..7 use
threadgroup (inter-warp) — O(log2 N) and parity-equivalent to
kernel.HypercubeGossipRef on CPU.

A neighbor j = lid ^ (1<<d) with j >= value_count is outside the
active grid; the reference skips that pair. OR/XOR with a neutral
operand is done by treating the neighbor contribution as 0 (do not
use nbr = lid, which would XOR a lane with itself to zero).

The band is chunked at GOSSIP_K_PER_CHUNK words per pass to fit
within the per-threadgroup memory budget. With GOSSIP_K_PER_CHUNK = 8
and the hard cap of 256 Values per community, one chunk consumes
8 * 256 * 8 = 16 KiB, under the 32 KiB M-series default.

Inactive lanes (lid >= value_count) participate in every barrier so
the threadgroup never deadlocks; their reads/writes are gated.
*/
#define GOSSIP_BAND_START_WORD   SIGNALS_START_WORD
#define GOSSIP_BAND_TARGET_WORD  ASSET_START_WORD
#define GOSSIP_BAND_WORDS        (SIGNALS_WORDS + CONTEXT_WORDS + GRADIENT_WORDS + PROPERTIES_WORDS)
#define GOSSIP_K_PER_CHUNK       8
#define GOSSIP_FULL_HYPERCUBE    256u

struct GossipParams {
    uint value_count;
    uint d_max;
    uint fold_op;
    uint _pad;
};

static inline ulong gossip_fold(ulong a, ulong b, uint foldOp) {
    return (foldOp == 0u) ? (a | b) : (a ^ b);
}

/* simd_shuffle_xor is 32b-wide; pair half-shuffles to move a 64b neighbor word. */
static inline ulong simd_shuffle_ulong_xor(ulong v, uint dmask) {
    uint lo = (uint)(v);
    uint hi = (uint)(v >> 32u);
    ushort m = (ushort)(dmask);
    uint pl = simd_shuffle_xor(lo, m);
    uint ph = simd_shuffle_xor(hi, m);
    return (ulong(pl) & 0xFFFFFFFFuL) | (ulong(ph) << 32u);
}

kernel void hypercube_gossip_kernel(
    device ulong* arena                 [[buffer(0)]],
    device const uint* indices          [[buffer(1)]],
    constant GossipParams& params       [[buffer(2)]],
    threadgroup ulong* shared           [[threadgroup(0)]],
    uint lid                            [[thread_position_in_threadgroup]]
) {
    bool active = lid < params.value_count;
    device ulong* frame = nullptr;
    if (active) {
        frame = arena + (uint64_t)indices[lid] * (uint64_t)WORDS;

        for (uint w = 0; w < GOSSIP_BAND_WORDS; w++) {
            frame[GOSSIP_BAND_TARGET_WORD + w] = frame[GOSSIP_BAND_START_WORD + w];
        }
    }

    threadgroup_barrier(mem_flags::mem_device | mem_flags::mem_threadgroup);

    for (uint chunkBase = 0; chunkBase < GOSSIP_BAND_WORDS; chunkBase += GOSSIP_K_PER_CHUNK) {
        uint chunkSize = GOSSIP_K_PER_CHUNK;
        if (chunkBase + chunkSize > GOSSIP_BAND_WORDS) {
            chunkSize = GOSSIP_BAND_WORDS - chunkBase;
        }

        if (active && frame && params.value_count == GOSSIP_FULL_HYPERCUBE) {
            /* Full hypercube: dims 0..4 in-warp, 5..7 over threadgroup (matches
               simdgroup width 32: masks 1,2,4,8,16 stay in-lane; 32+ cross warp). */
            ulong myChunk[GOSSIP_K_PER_CHUNK];
            for (uint w = 0; w < chunkSize; w++) {
                myChunk[w] = frame[GOSSIP_BAND_TARGET_WORD + chunkBase + w];
            }
            for (uint d = 0u; d < params.d_max && d < 5u; d++) {
                uint dmask = 1u << d;
                for (uint w = 0; w < chunkSize; w++) {
                    ulong peer = simd_shuffle_ulong_xor(myChunk[w], dmask);
                    myChunk[w] = gossip_fold(myChunk[w], peer, params.fold_op);
                }
            }
            for (uint d = 5u; d < params.d_max; d++) {
                uint dmask = 1u << d;
                for (uint w = 0; w < chunkSize; w++) {
                    shared[lid * GOSSIP_K_PER_CHUNK + w] = myChunk[w];
                }
                threadgroup_barrier(mem_flags::mem_threadgroup);
                for (uint w = 0; w < chunkSize; w++) {
                    uint nbr = lid ^ dmask;
                    ulong peer = shared[uint64_t(nbr) * GOSSIP_K_PER_CHUNK + w];
                    myChunk[w] = gossip_fold(myChunk[w], peer, params.fold_op);
                }
                threadgroup_barrier(mem_flags::mem_threadgroup);
            }
            for (uint w = 0; w < chunkSize; w++) {
                frame[GOSSIP_BAND_TARGET_WORD + chunkBase + w] = myChunk[w];
            }
        } else {
            if (active) {
                for (uint w = 0; w < chunkSize; w++) {
                    shared[lid * GOSSIP_K_PER_CHUNK + w] =
                        frame[GOSSIP_BAND_TARGET_WORD + chunkBase + w];
                }
            }
            threadgroup_barrier(mem_flags::mem_threadgroup);

            for (uint d = 0; d < params.d_max; d++) {
                uint mask = 1u << d;
                ulong nbrChunk[GOSSIP_K_PER_CHUNK];

                if (active) {
                    uint nbr = lid ^ mask;
                    bool nbrValid = nbr < params.value_count;
                    for (uint w = 0; w < chunkSize; w++) {
                        nbrChunk[w] = nbrValid
                            ? shared[uint64_t(nbr) * GOSSIP_K_PER_CHUNK + w]
                            : 0uL;
                    }
                }

                threadgroup_barrier(mem_flags::mem_threadgroup);

                if (active) {
                    for (uint w = 0; w < chunkSize; w++) {
                        ulong selfW = shared[lid * GOSSIP_K_PER_CHUNK + w];
                        ulong fl = gossip_fold(selfW, nbrChunk[w], params.fold_op);
                        shared[lid * GOSSIP_K_PER_CHUNK + w] = fl;
                    }
                }

                threadgroup_barrier(mem_flags::mem_threadgroup);
            }

            if (active) {
                for (uint w = 0; w < chunkSize; w++) {
                    frame[GOSSIP_BAND_TARGET_WORD + chunkBase + w] =
                        shared[lid * GOSSIP_K_PER_CHUNK + w];
                }
            }
        }

        threadgroup_barrier(mem_flags::mem_device | mem_flags::mem_threadgroup);
    }
}


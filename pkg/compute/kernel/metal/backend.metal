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

static inline Multivector sandwich(Multivector left, Multivector right) {
    return geometric_product(geometric_product(left, right), reverse(right));
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
hypercube_gossip_kernel executes the resident packed AST for one community.
Each program counter is staged into a compact post buffer and committed after
all targets are computed, matching the CPU write-ahead-log tick boundary
without allowing GPU write races to decide program semantics.
*/
#define AST_INVALID_INDEX          0xFFFFFFFFu
#define INSTR_DST_SPAN_SHIFT       0u
#define INSTR_DST_START_SHIFT      6u
#define INSTR_A_SPAN_SHIFT         13u
#define INSTR_A_START_SHIFT        19u
#define INSTR_B_SPAN_SHIFT         26u
#define INSTR_B_START_SHIFT        32u
#define INSTR_OPCODE_SHIFT         39u
#define INSTR_MODE_SHIFT           43u
#define INSTR_TOPOLOGY_SHIFT       46u
#define INSTR_PRED_START_SHIFT     48u
#define INSTR_PRED_COND_SHIFT      55u
#define INSTR_A_INDIRECT_SHIFT     57u
#define INSTR_B_TYPE_SHIFT         58u
#define INSTR_SPAN_MASK            0x3Fu
#define INSTR_START_MASK           0x7Fu
#define INSTR_FLAG_TARGET_B        (1ul << 60)
#define INSTR_FLAG_TARGET_OWNER    (1ul << 61)
#define INSTR_FLAG_A_FROM_B        (1ul << 62)
#define INSTR_FLAG_B_FROM_A        (1ul << 63)
#define MODE_TRUTH                 0u
#define MODE_POPCNT                1u
#define MODE_ANY_ZERO              2u
#define MODE_ALL_ONES              3u
#define MODE_GEOMETRIC             4u
#define MODE_EMIT                  5u
#define MODE_EMIT_FROM_OWNER       6u
#define MODE_MIN_NONZERO           7u
#define TOPOLOGY_SELF              0u
#define TOPOLOGY_NEXT              1u
#define TOPOLOGY_FOLD              2u
#define TOPOLOGY_SPAWN             3u
#define B_TYPE_DIRECT              0u
#define B_TYPE_INDIRECT            1u
#define B_TYPE_IMMEDIATE           2u
#define B_TYPE_NEXT                3u
#define PRED_EXTENDED              3u
#define PRED_KIND_POPCNT_LTE       1u
#define PRED_KIND_POPCNT_LT        2u
#define PRED_KIND_POPCNT_GTE       3u
#define PRED_KIND_POPCNT_GT        5u
#define PRED_KIND_HAMMING_LT       4u
#define PRED_KIND_HAMMING_LT_AND_EQ0 8u
#define PRED_KIND_HAMMING_LT_AND_NE0 9u
#define PRED_KIND_POPCNT_LT_AND_EQ0  10u
#define PRED_KIND_POPCNT_LT_AND_NE0  11u
#define PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT_AND_EQ0 12u
#define PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT_AND_NE0 13u
#define PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT 14u

struct AstParams {
    uint value_count;
    uint owner_index;
    uint owner_slot;
    uint _pad1;
};

static inline bool ast_index_active(constant AstParams& params, device const uint* indices, uint idx) {
    return idx < params.value_count && indices[idx] != AST_INVALID_INDEX;
}

static inline device ulong* ast_frame(device ulong* arena, device const uint* indices, uint idx) {
    return arena + (ulong)indices[idx] * (ulong)WORDS;
}

static inline uint ast_bits_len(uint value) {
    uint out = 0;
    while (value != 0) {
        out++;
        value >>= 1;
    }
    return out;
}

#define CURRENT_PRED_LT              0ul
#define CURRENT_PRED_LE              1ul
#define CURRENT_PRED_GT              2ul
#define CURRENT_PRED_GE              3ul
#define CURRENT_PRED_EQ              4ul
#define CURRENT_PRED_NE              5ul
#define CURRENT_PRED_STORE_POPCNT    6ul
#define CURRENT_PRED_ANY_ZERO        7ul
#define CURRENT_REDUCE_ARGMIN        1ul
#define CURRENT_REDUCE_MODE_EQ       2ul
#define CURRENT_MODE_TABLE_SIZE      1024u
#define CURRENT_REFERENCE_WORD       67u
#define CURRENT_SPAWN_REGISTER_WORD  70u

static inline device ulong* current_owner_frame(device ulong* arena, constant AstParams& params) {
    if (params.owner_slot == AST_INVALID_INDEX) {
        return nullptr;
    }

    return arena + (ulong)params.owner_slot * (ulong)WORDS;
}

static inline device ulong* current_next_b(
    device ulong* arena,
    device const uint* indices,
    constant AstParams& params,
    thread uint& queue_idx,
    thread uint& current_idx
) {
    while (queue_idx < params.value_count) {
        uint candidate = queue_idx;
        queue_idx++;

        if (!ast_index_active(params, indices, candidate)) {
            continue;
        }

        current_idx = candidate;

        return ast_frame(arena, indices, candidate);
    }

    current_idx = AST_INVALID_INDEX;

    return nullptr;
}

static inline ulong current_rotated_word(device ulong* frame, uint start, uint span, uint lane, ulong rotate) {
    uint idx = lane % span;
    ulong word = frame[(start + idx) & 127u];

    if (rotate == 0ul) {
        return word;
    }

    ulong shift = rotate * 8ul;
    ulong next = frame[(start + ((idx + 1u) % span)) & 127u];

    return (word >> shift) | (next << (64ul - shift));
}

static inline uint current_popcount(device ulong* frame, uint start, uint span) {
    uint out = 0u;

    for (uint lane = 0; lane < span; lane++) {
        out += popcount(frame[(start + lane) & 127u]);
    }

    return out;
}

static inline void current_reduce_argmin(
    device ulong* owner,
    device ulong* arena,
    device const uint* indices,
    constant AstParams& params,
    uint value_start,
    uint key_start,
    uint dst_start,
    uint guard_start
) {
    if (owner == nullptr || params.value_count == 0 || owner[guard_start & 127u] == 0ul) {
        return;
    }

    ulong best_value = 0ul;
    ulong best_key = ~0ul;

    for (uint idx = 0; idx < params.value_count; idx++) {
        if (!ast_index_active(params, indices, idx)) {
            continue;
        }

        device ulong* peer = ast_frame(arena, indices, idx);
        ulong value = peer[value_start & 127u];
        if (value == 0ul) {
            continue;
        }

        ulong key = peer[key_start & 127u];
        if (key >= best_key) {
            continue;
        }

        best_key = key;
        best_value = value;
    }

    if (best_value != 0ul) {
        owner[dst_start & 127u] = best_value;
    }
}

static inline void current_reduce_mode_eq(
    device ulong* owner,
    device ulong* arena,
    device const uint* indices,
    constant AstParams& params,
    uint value_start,
    uint key_start,
    uint dst_start,
    uint match_start
) {
    if (owner == nullptr || params.value_count == 0) {
        return;
    }

    ulong match = owner[match_start & 127u];
    if (match == 0ul) {
        return;
    }

    uchar used[CURRENT_MODE_TABLE_SIZE];
    ulong keys[CURRENT_MODE_TABLE_SIZE];
    uint counts[CURRENT_MODE_TABLE_SIZE];
    uint first_seen[CURRENT_MODE_TABLE_SIZE];

    for (uint slot = 0u; slot < CURRENT_MODE_TABLE_SIZE; slot++) {
        used[slot] = 0u;
        keys[slot] = 0ul;
        counts[slot] = 0u;
        first_seen[slot] = 0xFFFFFFFFu;
    }

    uint seen = 0u;
    for (uint idx = 0; idx < params.value_count; idx++) {
        if (!ast_index_active(params, indices, idx)) {
            continue;
        }

        device ulong* peer = ast_frame(arena, indices, idx);
        if (peer[key_start & 127u] != match) {
            continue;
        }

        ulong value = peer[value_start & 127u];
        if (value == 0ul) {
            continue;
        }

        uint slot = uint((value ^ (value >> 32ul)) & ulong(CURRENT_MODE_TABLE_SIZE - 1u));
        for (uint probe = 0u; probe < CURRENT_MODE_TABLE_SIZE; probe++) {
            if (used[slot] == 0u) {
                used[slot] = 1u;
                keys[slot] = value;
                counts[slot] = 1u;
                first_seen[slot] = seen;
                break;
            }

            if (keys[slot] == value) {
                counts[slot]++;
                break;
            }

            slot = (slot + 1u) & (CURRENT_MODE_TABLE_SIZE - 1u);
        }

        seen++;
    }

    ulong best_value = 0ul;
    uint best_count = 0u;
    uint best_order = 0xFFFFFFFFu;

    for (uint slot = 0u; slot < CURRENT_MODE_TABLE_SIZE; slot++) {
        if (used[slot] == 0u) {
            continue;
        }

        if (counts[slot] < best_count) {
            continue;
        }

        if (counts[slot] == best_count && first_seen[slot] >= best_order) {
            continue;
        }

        best_count = counts[slot];
        best_order = first_seen[slot];
        best_value = keys[slot];
    }

    if (best_value != 0ul) {
        owner[dst_start & 127u] = best_value;
    }
}

kernel void hypercube_gossip_kernel(
    device ulong* arena                 [[buffer(0)]],
    device const uint* indices          [[buffer(1)]],
    constant AstParams& params          [[buffer(2)]],
    device uint* stage_indices          [[buffer(3)]],
    device uint* stage_count            [[buffer(4)]],
    uint lid                            [[thread_position_in_threadgroup]]
) {
    // Only lid 0 executes because the resident program mutates one owner
    // frame, advances a shared pop(B) cursor, and may stage or emit in
    // sequential instruction order. The same guard also rejects empty
    // params.value_count and AST_INVALID_INDEX owner slots before any frame
    // access occurs.
    if (lid != 0u || params.value_count == 0u || params.owner_slot == AST_INVALID_INDEX) {
        return;
    }

    device ulong* owner = current_owner_frame(arena, params);
    if (owner == nullptr) {
        return;
    }

    if (stage_count != nullptr) {
        stage_count[0] = 0u;
    }

    uint b_queue_idx = 0u;
    uint current_b_idx = AST_INVALID_INDEX;
    uint pop_body_start = 0u;
    bool pop_active = false;
    device ulong* current_b = nullptr;

    for (uint pc = 0; pc < PROGRAM_WORDS; pc++) {
        ulong raw = owner[PROGRAM_START_WORD + pc];
        if (raw == 0ul) {
            continue;
        }

        ulong opcode = raw & 0xFul;
        uint a_start = uint((raw >> 4ul) & 0x7Ful);
        uint a_span = uint(((raw >> 11ul) & 0x7Ful) + 1ul);
        uint b_start = uint((raw >> 18ul) & 0x7Ful);
        uint b_span = uint(((raw >> 25ul) & 0x7Ful) + 1ul);
        uint dst_start = uint((raw >> 32ul) & 0x7Ful);
        uint dst_span = uint(((raw >> 39ul) & 0x7Ful) + 1ul);
        uint mask_start = uint((raw >> 46ul) & 0x7Ful);
        ulong target_b = (raw >> 53ul) & 1ul;
        ulong emit = (raw >> 54ul) & 1ul;
        ulong topology = (raw >> 55ul) & 3ul;
        ulong predicate = (raw >> 57ul) & 1ul;
        ulong pred_cond = (raw >> 58ul) & 7ul;
        ulong b_rotate = pred_cond;
        ulong src_a_from_b = (raw >> 61ul) & 1ul;
        ulong stage_bit = (raw >> 62ul) & 1ul;
        ulong pop_end = (raw >> 63ul) & 1ul;

        if (predicate == 1ul) {
            if (opcode == CURRENT_REDUCE_ARGMIN) {
                current_reduce_argmin(owner, arena, indices, params, a_start, b_start, dst_start, mask_start);
                continue;
            }
            if (opcode == CURRENT_REDUCE_MODE_EQ) {
                current_reduce_mode_eq(owner, arena, indices, params, a_start, b_start, dst_start, mask_start);
                continue;
            }
        }

        if (topology == 1ul && b_queue_idx < params.value_count) {
            current_b = current_next_b(arena, indices, params, b_queue_idx, current_b_idx);
            pop_body_start = pc + 1u;
            pop_active = current_b != nullptr;
        }

        if (stage_bit == 1ul) {
            if (current_b != nullptr && stage_indices != nullptr && stage_count != nullptr) {
                uint out_idx = stage_count[0];
                if (out_idx < params.value_count) {
                    stage_indices[out_idx] = current_b_idx;
                    stage_count[0] = out_idx + 1u;
                }
            }

            if (pop_end == 1ul && pop_active) {
                current_b = current_next_b(arena, indices, params, b_queue_idx, current_b_idx);
                if (current_b != nullptr) {
                    pc = pop_body_start - 1u;
                    continue;
                }

                pop_active = false;
            }

            continue;
        }

        device ulong* ptr_b = current_b;
        if (topology == 2ul && params.value_count > 0u) {
            uint dim_count = ast_bits_len(params.value_count - 1u);
            if (dim_count > 0u && params.owner_index != AST_INVALID_INDEX) {
                uint peer_idx = params.owner_index ^ 1u;
                if (peer_idx < params.value_count && ast_index_active(params, indices, peer_idx)) {
                    ptr_b = ast_frame(arena, indices, peer_idx);
                }
            }
        }
        if (ptr_b == nullptr) {
            ptr_b = owner;
        }

        device ulong* ptr_a = owner;
        if (src_a_from_b == 1ul) {
            ptr_a = ptr_b;
        }

        device ulong* ptr_dst = owner;
        if (target_b == 1ul) {
            ptr_dst = ptr_b;
        }
        if (ptr_dst == nullptr || ptr_a == nullptr || ptr_b == nullptr) {
            continue;
        }

        if (predicate == 1ul) {
            ulong guard = owner[mask_start & 127u];
            ulong pop = ulong(current_popcount(ptr_a, a_start, a_span));

            if (pred_cond == CURRENT_PRED_STORE_POPCNT) {
                uint dst_idx = dst_start & 127u;
                ulong prev_dst = ptr_dst[dst_idx];
                ptr_dst[dst_idx] = (pop & guard) | (prev_dst & ~guard);
            } else if (pred_cond == CURRENT_PRED_ANY_ZERO) {
                bool zero_seen = false;
                for (uint lane = 0; lane < a_span; lane++) {
                    if (ptr_a[(a_start + lane) & 127u] == 0ul) {
                        zero_seen = true;
                        break;
                    }
                }

                ulong result = zero_seen ? ~0ul : 0ul;
                uint dst_idx = dst_start & 127u;
                ulong prev_dst = ptr_dst[dst_idx];
                ptr_dst[dst_idx] = (result & guard) | (prev_dst & ~guard);
            } else {
                ulong threshold = owner[b_start & 127u];
                ulong witness = a_span == 1u ? ptr_a[a_start & 127u] : pop;
                bool hit = false;

                if (pred_cond == CURRENT_PRED_LT) {
                    hit = witness < threshold;
                } else if (pred_cond == CURRENT_PRED_LE) {
                    hit = witness <= threshold;
                } else if (pred_cond == CURRENT_PRED_GT) {
                    hit = witness > threshold;
                } else if (pred_cond == CURRENT_PRED_GE) {
                    hit = witness >= threshold;
                } else if (pred_cond == CURRENT_PRED_EQ) {
                    hit = witness == threshold;
                } else if (pred_cond == CURRENT_PRED_NE) {
                    hit = witness != threshold;
                }

                ptr_dst[dst_start & 127u] = (hit ? ~0ul : 0ul) & guard;
            }

            if (pop_end == 1ul && pop_active) {
                current_b = current_next_b(arena, indices, params, b_queue_idx, current_b_idx);
                if (current_b != nullptr) {
                    pc = pop_body_start - 1u;
                    continue;
                }

                pop_active = false;
            }

            continue;
        }

        ulong mask = owner[mask_start & 127u];
        ulong m0 = (opcode & 1ul) != 0ul ? ~0ul : 0ul;
        ulong m1 = (opcode & 2ul) != 0ul ? ~0ul : 0ul;
        ulong m2 = (opcode & 4ul) != 0ul ? ~0ul : 0ul;
        ulong m3 = (opcode & 8ul) != 0ul ? ~0ul : 0ul;
        bool hypercube = topology == 2ul && params.value_count > 0u;

        if (hypercube && target_b == 1ul) {
            for (uint peer_idx = 0; peer_idx < params.value_count; peer_idx++) {
                if (peer_idx == params.owner_index || !ast_index_active(params, indices, peer_idx)) {
                    continue;
                }

                device ulong* peer = ast_frame(arena, indices, peer_idx);
                for (uint lane = 0; lane < dst_span; lane++) {
                    ulong word_a = ptr_a[(a_start + (lane % a_span)) & 127u];
                    ulong word_b = current_rotated_word(peer, b_start, b_span, lane, b_rotate);
                    ulong res = (word_a & word_b & m0) |
                        (word_a & ~word_b & m1) |
                        (~word_a & word_b & m2) |
                        (~word_a & ~word_b & m3);

                    uint dst_idx = (dst_start + lane) & 127u;
                    ulong prev_dst = peer[dst_idx];
                    peer[dst_idx] = (res & mask) | (prev_dst & ~mask);
                }
            }
        } else {
            uint peers = hypercube ? params.value_count : 1u;
            for (uint lane = 0; lane < dst_span; lane++) {
                ulong start_a = ptr_a[(a_start + (lane % a_span)) & 127u];
                uint dst_idx = (dst_start + lane) & 127u;
                ulong prev_dst = ptr_dst[dst_idx];
                ulong acc = start_a;
                bool any = false;

                for (uint peer_idx = 0; peer_idx < peers; peer_idx++) {
                    device ulong* peer = ptr_b;
                    if (hypercube) {
                        if (peer_idx == params.owner_index || !ast_index_active(params, indices, peer_idx)) {
                            continue;
                        }

                        peer = ast_frame(arena, indices, peer_idx);
                    }

                    ulong word_b = current_rotated_word(peer, b_start, b_span, lane, b_rotate);
                    acc = (acc & word_b & m0) |
                        (acc & ~word_b & m1) |
                        (~acc & word_b & m2) |
                        (~acc & ~word_b & m3);
                    any = true;
                }

                if (!any) {
                    acc = start_a;
                }

                ptr_dst[dst_idx] = (acc & mask) | (prev_dst & ~mask);
            }
        }

        if (emit == 1ul && mask != 0ul) {
            owner[CURRENT_SPAWN_REGISTER_WORD] += 1ul;
        }

        if (pop_end == 1ul && pop_active) {
            current_b = current_next_b(arena, indices, params, b_queue_idx, current_b_idx);
            if (current_b != nullptr) {
                pc = pop_body_start - 1u;
                continue;
            }

            pop_active = false;
        }
    }
}

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

struct AstParams {
    uint value_count;
    uint owner_index;
    uint _pad0;
    uint _pad1;
};

struct DecodedInstr {
    int a_start;
    int a_span;
    int b_start;
    int b_span;
    int dst_start;
    int dst_span;
    ulong opcode;
    ulong mode;
    ulong topology;
    ulong pred_start;
    ulong pred_cond;
    ulong a_ind;
    ulong b_type;
};

struct PredicateSpec {
    uint kind;
    uint start;
    uint span;
    ulong threshold;
};

struct ExecContext {
    device ulong* exec;
    device ulong* a;
    device ulong* b;
};

static inline bool ast_index_active(constant AstParams& params, device const uint* indices, uint idx) {
    return idx < params.value_count && indices[idx] != AST_INVALID_INDEX;
}

static inline device ulong* ast_frame(device ulong* arena, device const uint* indices, uint idx) {
    return arena + (ulong)indices[idx] * (ulong)WORDS;
}

static inline ulong ast_truth(ulong opcode, ulong a, ulong b) {
    ulong m0 = (opcode & 1ul) != 0 ? ~0ul : 0ul;
    ulong m1 = (opcode & 2ul) != 0 ? ~0ul : 0ul;
    ulong m2 = (opcode & 4ul) != 0 ? ~0ul : 0ul;
    ulong m3 = (opcode & 8ul) != 0 ? ~0ul : 0ul;

    return (a & b & m0) | (a & ~b & m1) | (~a & b & m2) | (~a & ~b & m3);
}

static inline DecodedInstr ast_decode(ulong instr) {
    DecodedInstr out;
    out.dst_span = int((instr >> INSTR_DST_SPAN_SHIFT) & INSTR_SPAN_MASK) + 1;
    out.dst_start = int((instr >> INSTR_DST_START_SHIFT) & INSTR_START_MASK);
    out.a_span = int((instr >> INSTR_A_SPAN_SHIFT) & INSTR_SPAN_MASK) + 1;
    out.a_start = int((instr >> INSTR_A_START_SHIFT) & INSTR_START_MASK);
    out.b_span = int((instr >> INSTR_B_SPAN_SHIFT) & INSTR_SPAN_MASK) + 1;
    out.b_start = int((instr >> INSTR_B_START_SHIFT) & INSTR_START_MASK);
    out.opcode = (instr >> INSTR_OPCODE_SHIFT) & 0xFul;
    out.mode = (instr >> INSTR_MODE_SHIFT) & 0x7ul;
    if (out.mode == MODE_GEOMETRIC) {
        out.opcode <<= 4;
    }
    out.topology = (instr >> INSTR_TOPOLOGY_SHIFT) & 0x3ul;
    out.pred_start = (instr >> INSTR_PRED_START_SHIFT) & INSTR_START_MASK;
    out.pred_cond = (instr >> INSTR_PRED_COND_SHIFT) & 0x3ul;
    out.a_ind = (instr >> INSTR_A_INDIRECT_SHIFT) & 0x1ul;
    out.b_type = (instr >> INSTR_B_TYPE_SHIFT) & 0x3ul;
    return out;
}

static inline uint ast_bits_len(uint value) {
    uint out = 0;
    while (value != 0) {
        out++;
        value >>= 1;
    }
    return out;
}

static inline int ast_route(uint source_idx, constant AstParams& params, DecodedInstr instr, ulong raw, uint pc) {
    int target = int(source_idx);

    if ((raw & INSTR_FLAG_TARGET_OWNER) != 0 && params.owner_index != AST_INVALID_INDEX) {
        target = int(params.owner_index);
    }
    if ((raw & INSTR_FLAG_TARGET_B) != 0) {
        target = int(source_idx);
    }

    if (instr.topology == TOPOLOGY_NEXT && params.value_count > 1) {
        uint max_dim = ast_bits_len(params.value_count - 1u);
        uint mask = 1u << (pc % max_dim);
        uint routed = source_idx ^ mask;
        if (routed < params.value_count) {
            target = int(routed);
        }
    }

    if (instr.topology == TOPOLOGY_SPAWN) {
        return -1;
    }

    return target;
}

static inline bool ast_predicate_allows(device ulong* frame, DecodedInstr instr, device const PredicateSpec* specs) {
    if (instr.pred_cond == 0) {
        return true;
    }
    if (frame == nullptr) {
        return false;
    }
    if (instr.pred_cond == 1) {
        return frame[instr.pred_start] != 0;
    }
    if (instr.pred_cond == 2) {
        return frame[instr.pred_start] == 0;
    }

    PredicateSpec spec = specs[instr.pred_start];
    if (spec.kind != PRED_KIND_POPCNT_LTE) {
        return frame[instr.pred_start] > 0;
    }

    uint count = 0;
    for (uint lane = 0; lane < spec.span && spec.start + lane < WORDS; lane++) {
        count += popcount(frame[spec.start + lane]);
    }

    return ulong(count) <= spec.threshold;
}

static inline ExecContext ast_context(
    device ulong* arena,
    device const uint* indices,
    constant AstParams& params,
    uint source_idx,
    uint pc,
    ulong raw,
    DecodedInstr instr
) {
    device ulong* source = ast_frame(arena, indices, source_idx);
    device ulong* owner = params.owner_index == AST_INVALID_INDEX ? nullptr : ast_frame(arena, indices, params.owner_index);

    ExecContext ctx;
    ctx.exec = owner == nullptr ? source : owner;
    ctx.a = owner == nullptr ? source : owner;
    ctx.b = source;

    if (owner != nullptr && (ctx.exec[PROGRAM_START_WORD + pc] & INSTR_FLAG_A_FROM_B) != 0) {
        ctx.a = source;
    }
    if (owner != nullptr && (raw & INSTR_FLAG_A_FROM_B) != 0) {
        ctx.a = source;
    }
    if (owner != nullptr && (raw & INSTR_FLAG_B_FROM_A) != 0) {
        ctx.b = owner;
    }
    if (instr.b_type == B_TYPE_NEXT) {
        uint next = source_idx + 1u;
        ctx.b = ast_index_active(params, indices, next) ? ast_frame(arena, indices, next) : nullptr;
    }

    return ctx;
}

static inline void ast_geometric(DecodedInstr instr, device ulong* a_frame, device ulong* b_frame, thread ulong out[8]) {
    ulong tmp[WORDS];
    for (uint lane = 0; lane < WORDS; lane++) {
        tmp[lane] = 0;
    }
    for (int lane = 0; lane < instr.a_span && lane < CONTEXT_WORDS; lane++) {
        int idx = instr.a_start + lane;
        if (idx < WORDS) {
            tmp[CONTEXT_START_WORD + lane] = a_frame[idx];
        }
    }
    if (b_frame != nullptr) {
        for (int lane = 0; lane < instr.b_span && lane < GRADIENT_WORDS; lane++) {
            int idx = instr.b_start + lane;
            if (idx < WORDS) {
                tmp[GRADIENT_START_WORD + lane] = b_frame[idx];
            }
        }
    }

    Multivector left;
    Multivector right;
    for (uint lane = 0; lane < 8; lane++) {
        left.v[lane] = double_word_to_float(tmp[CONTEXT_START_WORD + lane]);
        right.v[lane] = double_word_to_float(tmp[GRADIENT_START_WORD + lane]);
    }

    Multivector mv;
    if (instr.opcode == OPCODE_GEOMETRIC_COMPOSE) {
        mv = geometric_product(left, right);
    } else if (instr.opcode == OPCODE_GEOMETRIC_SANDWICH) {
        mv = sandwich(left, right);
    } else {
        mv = reverse(left);
    }
    for (uint lane = 0; lane < 8; lane++) {
        out[lane] = float_to_double_word(mv.v[lane]);
    }
}

static inline int ast_payloads(DecodedInstr instr, ExecContext ctx, thread ulong final_res[64]) {
    if (instr.a_ind == 1 && ctx.a != nullptr) {
        instr.a_start = int(ctx.a[instr.a_start] & 0x7Ful);
    }
    if (instr.b_type == B_TYPE_INDIRECT && ctx.b != nullptr) {
        instr.b_start = int(ctx.b[instr.b_start] & 0x7Ful);
    }

    if (instr.mode == MODE_GEOMETRIC || (instr.opcode & 0xF0ul) != 0) {
        ast_geometric(instr, ctx.a, ctx.b, final_res);
        return min(instr.dst_span, SIGNALS_WORDS);
    }

    ulong b_imm = ulong(instr.b_start) | (ulong(instr.b_span - 1) << 7);
    int lanes = instr.dst_span;
    if (instr.mode != MODE_TRUTH && instr.mode != MODE_EMIT) {
        lanes = max(instr.a_span, instr.b_span);
    }

    ulong truth[64];
    for (int lane = 0; lane < lanes; lane++) {
        ulong a = 0;
        ulong b = 0;
        int a_idx = instr.a_start + (lane % instr.a_span);
        if (ctx.a != nullptr && a_idx < WORDS) {
            a = ctx.a[a_idx];
        }
        if (instr.b_type == B_TYPE_IMMEDIATE) {
            b = b_imm;
        } else {
            int b_idx = instr.b_start + (lane % instr.b_span);
            if (ctx.b != nullptr && b_idx < WORDS) {
                b = ctx.b[b_idx];
            }
        }
        truth[lane] = ast_truth(instr.opcode, a, b);
    }

    if (instr.mode == MODE_POPCNT) {
        uint total = 0;
        for (int lane = 0; lane < lanes; lane++) {
            total += popcount(truth[lane]);
        }
        final_res[0] = total;
        return 1;
    }
    if (instr.mode == MODE_ANY_ZERO) {
        ulong witness = 0;
        for (int lane = 0; lane < lanes; lane++) {
            if (truth[lane] != ~0ul) {
                witness = 1;
            }
        }
        final_res[0] = witness;
        return 1;
    }
    if (instr.mode == MODE_ALL_ONES) {
        ulong witness = 1;
        for (int lane = 0; lane < lanes; lane++) {
            if (truth[lane] != ~0ul) {
                witness = 0;
            }
        }
        final_res[0] = witness;
        return 1;
    }

    for (int lane = 0; lane < lanes; lane++) {
        final_res[lane] = truth[lane];
    }
    return lanes;
}

static inline ulong ast_fold_payload(
    device ulong* arena,
    device const uint* indices,
    constant AstParams& params,
    device const PredicateSpec* specs,
    uint pc,
    ulong opcode,
    int dst_idx
) {
    bool seeded = false;
    ulong aggregate = 0;

    for (uint source = 0; source < params.value_count; source++) {
        if (!ast_index_active(params, indices, source)) {
            continue;
        }

        device ulong* exec = params.owner_index == AST_INVALID_INDEX ? ast_frame(arena, indices, source) : ast_frame(arena, indices, params.owner_index);
        ulong raw = exec[PROGRAM_START_WORD + pc];
        if (raw == 0) {
            continue;
        }

        DecodedInstr instr = ast_decode(raw);
        if (instr.topology != TOPOLOGY_FOLD || instr.opcode != opcode || dst_idx < instr.dst_start || dst_idx >= instr.dst_start + instr.dst_span) {
            continue;
        }

        ExecContext ctx = ast_context(arena, indices, params, source, pc, raw, instr);
        if (!ast_predicate_allows(ctx.b, instr, specs)) {
            continue;
        }

        ulong final_res[64];
        int final_len = ast_payloads(instr, ctx, final_res);
        ulong payload = final_len == 1 ? final_res[0] : final_res[dst_idx - instr.dst_start];

        if (!seeded) {
            aggregate = payload;
            seeded = true;
            continue;
        }

        aggregate = ast_truth(opcode, aggregate, payload);
    }

    return aggregate;
}

static inline void ast_initialize_spawn(
    device ulong* arena,
    device const uint* spawn_indices,
    device const ulong* spawn_ids,
    device uchar* spawn_active,
    uint source_idx,
    device ulong* source
) {
    if (spawn_indices == nullptr || spawn_indices[source_idx] == AST_INVALID_INDEX) {
        return;
    }
    if (spawn_active[source_idx] != 0) {
        return;
    }

    device ulong* spawned = arena + (ulong)spawn_indices[source_idx] * (ulong)WORDS;
    for (uint word = 0; word < WORDS; word++) {
        spawned[word] = source[word];
    }
    spawned[ID_START_WORD] = spawn_ids[source_idx];
    for (uint word = 0; word < PROGRAM_WORDS; word++) {
        spawned[PROGRAM_START_WORD + word] = 0UL;
    }
    spawned[SCHEDULING_NEXT_PROGRAM_WORD] = 0UL;
    spawned[PROPERTIES_STATUS_WORD] = STATUS_PENDING;
    spawn_active[source_idx] = 1;
}

kernel void hypercube_gossip_kernel(
    device ulong* arena                 [[buffer(0)]],
    device const uint* indices          [[buffer(1)]],
    constant AstParams& params          [[buffer(2)]],
    device const PredicateSpec* specs   [[buffer(3)]],
    device const uint* spawn_indices    [[buffer(4)]],
    device const ulong* spawn_ids       [[buffer(5)]],
    device ulong* post                  [[buffer(6)]],
    device uchar* spawn_active          [[buffer(7)]],
    uint lid                            [[thread_position_in_threadgroup]]
) {
    bool target_active = ast_index_active(params, indices, lid);
    device ulong* target_frame = target_active ? ast_frame(arena, indices, lid) : nullptr;
    device ulong* target_post = post + (ulong)lid * (ulong)WORDS;

    for (uint pc = 0; pc < PROGRAM_WORDS; pc++) {
        for (uint word = 0; word < WORDS; word++) {
            target_post[word] = target_active ? target_frame[word] : 0;
        }

        threadgroup_barrier(mem_flags::mem_device | mem_flags::mem_threadgroup);

        if (target_active) {
            for (uint source = 0; source < params.value_count; source++) {
                if (!ast_index_active(params, indices, source)) {
                    continue;
                }

                device ulong* exec = params.owner_index == AST_INVALID_INDEX ? ast_frame(arena, indices, source) : ast_frame(arena, indices, params.owner_index);
                ulong raw = exec[PROGRAM_START_WORD + pc];
                if (raw == 0) {
                    continue;
                }

                DecodedInstr instr = ast_decode(raw);
                if (instr.topology == TOPOLOGY_SPAWN) {
                    continue;
                }

                ExecContext ctx = ast_context(arena, indices, params, source, pc, raw, instr);
                if (!ast_predicate_allows(ctx.b, instr, specs)) {
                    continue;
                }

                int route = ast_route(source, params, instr, raw, pc);
                if (route != int(lid)) {
                    continue;
                }

                ulong final_res[64];
                int final_len = ast_payloads(instr, ctx, final_res);
                for (int lane = 0; lane < instr.dst_span; lane++) {
                    int dst = instr.dst_start + lane;
                    if (dst >= WORDS) {
                        continue;
                    }

                    ulong payload = final_len == 1 ? final_res[0] : (lane < final_len ? final_res[lane] : 0);
                    if (instr.topology == TOPOLOGY_FOLD) {
                        payload = ast_fold_payload(arena, indices, params, specs, pc, instr.opcode, dst);
                    }
                    target_post[dst] = payload;
                }
            }
        }

        if (target_active) {
            device ulong* source = target_frame;
            device ulong* exec = params.owner_index == AST_INVALID_INDEX ? source : ast_frame(arena, indices, params.owner_index);
            ulong raw = exec[PROGRAM_START_WORD + pc];
            if (raw != 0) {
                DecodedInstr instr = ast_decode(raw);
                if (instr.topology == TOPOLOGY_SPAWN) {
                    ExecContext ctx = ast_context(arena, indices, params, lid, pc, raw, instr);
                    if (ast_predicate_allows(ctx.b, instr, specs)) {
                        ast_initialize_spawn(arena, spawn_indices, spawn_ids, spawn_active, lid, source);
                        if (spawn_active != nullptr && spawn_active[lid] != 0) {
                            device ulong* spawned = arena + (ulong)spawn_indices[lid] * (ulong)WORDS;
                            ulong final_res[64];
                            int final_len = ast_payloads(instr, ctx, final_res);
                            for (int lane = 0; lane < instr.dst_span; lane++) {
                                int dst = instr.dst_start + lane;
                                if (dst < WORDS) {
                                    spawned[dst] = final_len == 1 ? final_res[0] : (lane < final_len ? final_res[lane] : 0);
                                }
                            }
                        }
                    }
                }
            }
        }

        threadgroup_barrier(mem_flags::mem_device | mem_flags::mem_threadgroup);

        if (target_active) {
            for (uint word = 0; word < WORDS; word++) {
                target_frame[word] = target_post[word];
            }
        }

        threadgroup_barrier(mem_flags::mem_device | mem_flags::mem_threadgroup);
    }
}

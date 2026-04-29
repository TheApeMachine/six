#include <cuda_runtime.h>
#include <stdint.h>
#include <stdio.h>
#include "../shared/primitives.h"
#include "../shared/postexec_layout.h"

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
    double v[8];
};

static __device__ __forceinline__ double word_to_double(uint64_t word) {
    return __longlong_as_double((long long)word);
}

static __device__ __forceinline__ uint64_t double_to_word(double value) {
    return (uint64_t)__double_as_longlong(value);
}

static __device__ __forceinline__ Multivector load_multivector(uint64_t* frame, int start) {
    Multivector mv;
    #pragma unroll
    for (int idx = 0; idx < 8; idx++) {
        mv.v[idx] = word_to_double(frame[start + idx]);
    }
    return mv;
}

static __device__ __forceinline__ void store_multivector(uint64_t* frame, int start, Multivector mv) {
    #pragma unroll
    for (int idx = 0; idx < 8; idx++) {
        frame[start + idx] = double_to_word(mv.v[idx]);
    }
}

static __device__ __forceinline__ Multivector geometric_product(Multivector left, Multivector right) {
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

static __device__ __forceinline__ Multivector reverse(Multivector mv) {
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

static __device__ __forceinline__ Multivector sandwich(Multivector left, Multivector right) {
    return geometric_product(geometric_product(left, right), reverse(right));
}

static __device__ __forceinline__ void unpack_region_ref(uint64_t word, int* start, int* span) {
    *start = (int)(uint32_t)word;
    *span = (int)(uint32_t)(word >> 32);
}

static __device__ __forceinline__ uint64_t exact_binary_word(uint64_t op, uint64_t a, uint64_t b) {
    uint64_t m0 = (op & 1) ? ~0ULL : 0ULL;
    uint64_t m1 = (op & 2) ? ~0ULL : 0ULL;
    uint64_t m2 = (op & 4) ? ~0ULL : 0ULL;
    uint64_t m3 = (op & 8) ? ~0ULL : 0ULL;
    return (a & b & m0) |
           (a & ~b & m1) |
           (~a & b & m2) |
           (~a & ~b & m3);
}

static __device__ __forceinline__ void copy_mask_merge_device(uint64_t* frame) {
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
        uint64_t mask = frame[bStart + idx];
        uint64_t src = frame[aStart + idx];
        int dst = dstStart + idx;
        frame[dst] = (src & mask) | (frame[dst] & ~mask);
    }
}

static __device__ __forceinline__ void exact_binary_device(
    uint64_t* frame,
    uint64_t op,
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

__global__ void geometric_kernel(uint64_t* A, uint32_t num_values) {
    uint32_t id = blockIdx.x * blockDim.x + threadIdx.x;
    if (id >= num_values) return;

    uint32_t base = id * WORDS;
    uint64_t* frame = A + base;
    uint8_t op = (uint8_t)(frame[PROGRAM_START_WORD] & OPCODE_GEOMETRIC_MASK);

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

#define AST_INVALID_INDEX          0xFFFFFFFFu
#define AST_DST_SPAN_SHIFT         0u
#define AST_DST_START_SHIFT        6u
#define AST_A_SPAN_SHIFT           13u
#define AST_A_START_SHIFT          19u
#define AST_B_SPAN_SHIFT           26u
#define AST_B_START_SHIFT          32u
#define AST_OPCODE_SHIFT           39u
#define AST_MODE_SHIFT             43u
#define AST_TOPOLOGY_SHIFT         46u
#define AST_PRED_START_SHIFT       48u
#define AST_PRED_COND_SHIFT        55u
#define AST_A_INDIRECT_SHIFT       57u
#define AST_B_TYPE_SHIFT           58u
#define AST_SPAN_MASK              0x3Fu
#define AST_START_MASK             0x7Fu
#define AST_FLAG_TARGET_B          (1ULL << 60)
#define AST_FLAG_TARGET_OWNER      (1ULL << 61)
#define AST_FLAG_A_FROM_B          (1ULL << 62)
#define AST_FLAG_B_FROM_A          (1ULL << 63)
#define AST_MODE_TRUTH             0u
#define AST_MODE_POPCNT            1u
#define AST_MODE_ANY_ZERO          2u
#define AST_MODE_ALL_ONES          3u
#define AST_MODE_GEOMETRIC         4u
#define AST_MODE_EMIT              5u
#define AST_MODE_EMIT_FROM_OWNER   6u
#define AST_MODE_MIN_NONZERO       7u
#define AST_TOPOLOGY_SELF          0u
#define AST_TOPOLOGY_NEXT          1u
#define AST_TOPOLOGY_FOLD          2u
#define AST_TOPOLOGY_SPAWN         3u
#define AST_B_TYPE_DIRECT          0u
#define AST_B_TYPE_INDIRECT        1u
#define AST_B_TYPE_IMMEDIATE       2u
#define AST_B_TYPE_NEXT            3u
#define AST_PRED_KIND_POPCNT_LTE   1u
#define AST_PRED_KIND_POPCNT_LT    2u
#define AST_PRED_KIND_POPCNT_GTE   3u
#define AST_PRED_KIND_POPCNT_GT    5u
#define AST_PRED_KIND_HAMMING_LT   4u
#define AST_PRED_KIND_HAMMING_LT_AND_EQ0 8u
#define AST_PRED_KIND_HAMMING_LT_AND_NE0 9u
#define AST_PRED_KIND_POPCNT_LT_AND_EQ0  10u
#define AST_PRED_KIND_POPCNT_LT_AND_NE0  11u
#define AST_PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT_AND_EQ0 12u
#define AST_PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT_AND_NE0 13u
#define AST_PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT 14u

typedef struct {
    uint64_t kind;
    uint64_t start;
    uint64_t span;
    uint64_t threshold;
    uint64_t and_word;
    uint64_t threshold_b;
} predicate_device_spec_t;

typedef struct {
    int a_start;
    int a_span;
    int b_start;
    int b_span;
    int dst_start;
    int dst_span;
    uint64_t opcode;
    uint64_t mode;
    uint64_t topology;
    uint64_t pred_start;
    uint64_t pred_cond;
    uint64_t a_ind;
    uint64_t b_type;
} ast_decoded_instr;

typedef struct {
    uint64_t* exec;
    uint64_t* a;
    uint64_t* b;
} ast_context;

static __device__ __forceinline__ uint64_t ast_truth_word(uint64_t opcode, uint64_t a, uint64_t b) {
    uint64_t m0 = (opcode & 1ULL) ? ~0ULL : 0ULL;
    uint64_t m1 = (opcode & 2ULL) ? ~0ULL : 0ULL;
    uint64_t m2 = (opcode & 4ULL) ? ~0ULL : 0ULL;
    uint64_t m3 = (opcode & 8ULL) ? ~0ULL : 0ULL;
    return (a & b & m0) | (a & ~b & m1) | (~a & b & m2) | (~a & ~b & m3);
}

static __device__ __forceinline__ ast_decoded_instr ast_decode(uint64_t instr) {
    ast_decoded_instr out;
    out.dst_span = (int)((instr >> AST_DST_SPAN_SHIFT) & AST_SPAN_MASK) + 1;
    out.dst_start = (int)((instr >> AST_DST_START_SHIFT) & AST_START_MASK);
    out.a_span = (int)((instr >> AST_A_SPAN_SHIFT) & AST_SPAN_MASK) + 1;
    out.a_start = (int)((instr >> AST_A_START_SHIFT) & AST_START_MASK);
    out.b_span = (int)((instr >> AST_B_SPAN_SHIFT) & AST_SPAN_MASK) + 1;
    out.b_start = (int)((instr >> AST_B_START_SHIFT) & AST_START_MASK);
    out.opcode = (instr >> AST_OPCODE_SHIFT) & 0xFULL;
    out.mode = (instr >> AST_MODE_SHIFT) & 0x7ULL;
    if (out.mode == AST_MODE_GEOMETRIC) out.opcode <<= 4;
    out.topology = (instr >> AST_TOPOLOGY_SHIFT) & 0x3ULL;
    out.pred_start = (instr >> AST_PRED_START_SHIFT) & AST_START_MASK;
    out.pred_cond = (instr >> AST_PRED_COND_SHIFT) & 0x3ULL;
    out.a_ind = (instr >> AST_A_INDIRECT_SHIFT) & 0x1ULL;
    out.b_type = (instr >> AST_B_TYPE_SHIFT) & 0x3ULL;
    return out;
}

static __device__ __forceinline__ uint32_t ast_bits_len(uint32_t value) {
    uint32_t out = 0;
    while (value != 0) {
        out++;
        value >>= 1;
    }
    return out;
}

static __device__ __forceinline__ uint64_t* ast_frame(uint64_t* frames, uint32_t idx) {
    return frames + (uint64_t)idx * (uint64_t)WORDS;
}

static __device__ __forceinline__ bool ast_active(const uint8_t* active, uint32_t count, uint32_t idx) {
    return idx < count && active[idx] != 0;
}

static __device__ __forceinline__ int ast_route(uint32_t source_idx, uint32_t value_count, uint32_t owner_index, uint32_t pc, ast_decoded_instr instr, uint64_t raw) {
    int target = (int)source_idx;

    if ((raw & AST_FLAG_TARGET_OWNER) != 0 && owner_index != AST_INVALID_INDEX) target = (int)owner_index;
    if ((raw & AST_FLAG_TARGET_B) != 0) target = (int)source_idx;

    if (instr.topology == AST_TOPOLOGY_NEXT && value_count > 1) {
        uint32_t max_dim = ast_bits_len(value_count - 1u);
        uint32_t routed = source_idx ^ (1u << (pc % max_dim));
        if (routed < value_count) target = (int)routed;
    }
    if (instr.topology == AST_TOPOLOGY_SPAWN) return -1;

    return target;
}

static __device__ __forceinline__ ast_context ast_make_context(
    uint64_t* frames,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t owner_index,
    uint32_t source_idx,
    uint32_t pc,
    uint64_t raw,
    ast_decoded_instr instr
) {
    uint64_t* source = ast_frame(frames, source_idx);
    uint64_t* owner = owner_index == AST_INVALID_INDEX ? nullptr : ast_frame(frames, owner_index);

    ast_context ctx;
    ctx.exec = owner == nullptr ? source : owner;
    ctx.a = owner == nullptr ? source : owner;
    ctx.b = source;

    if (owner != nullptr && (ctx.exec[PROGRAM_START_WORD + pc] & AST_FLAG_A_FROM_B) != 0) ctx.a = source;
    if (owner != nullptr && (raw & AST_FLAG_A_FROM_B) != 0) ctx.a = source;
    if (owner != nullptr && (raw & AST_FLAG_B_FROM_A) != 0) ctx.b = owner;
    if (instr.b_type == AST_B_TYPE_NEXT) {
        uint32_t next = source_idx + 1u;
        ctx.b = ast_active(active, value_count, next) ? ast_frame(frames, next) : nullptr;
    } else if (instr.b_type == AST_B_TYPE_DIRECT && owner != nullptr && source_idx == owner_index &&
               instr.topology == AST_TOPOLOGY_NEXT && value_count > 1u && (raw & AST_FLAG_B_FROM_A) == 0) {
        uint32_t max_dim = ast_bits_len(value_count - 1u);
        uint32_t peer = source_idx ^ (1u << (pc % max_dim));
        if (peer < value_count && ast_active(active, value_count, peer)) {
            ctx.b = ast_frame(frames, peer);
        }
    }

    return ctx;
}

static __device__ __forceinline__ uint32_t ast_hamming_distance(
    const uint64_t* frame_a,
    const uint64_t* frame_b,
    uint32_t start,
    uint32_t span,
    uint32_t word_count
) {
    uint32_t dist = 0;
    for (uint32_t lane = 0; lane < span && start + lane < word_count; lane++) {
        dist += (uint32_t)__popcll((unsigned long long)(frame_a[start + lane] ^ frame_b[start + lane]));
    }
    return dist;
}

static __device__ __forceinline__ uint32_t ast_union_popcount(
    const uint64_t* frame_a,
    const uint64_t* frame_b,
    uint32_t start,
    uint32_t span,
    uint32_t word_count
) {
    uint32_t count = 0;
    for (uint32_t lane = 0; lane < span && start + lane < word_count; lane++) {
        count += (uint32_t)__popcll((unsigned long long)(frame_a[start + lane] | frame_b[start + lane]));
    }
    return count;
}

static __device__ __forceinline__ bool ast_predicate_allows(
    uint64_t* frame,
    ast_decoded_instr instr,
    const predicate_device_spec_t* specs,
    uint64_t* frame_a,
    uint64_t* frame_b
) {
    if (instr.pred_cond == 0) return true;
    if (frame == nullptr) return false;
    if (instr.pred_cond == 1) return frame[instr.pred_start] != 0;
    if (instr.pred_cond == 2) return frame[instr.pred_start] == 0;

    predicate_device_spec_t spec = specs[instr.pred_start];
    if (spec.kind == AST_PRED_KIND_HAMMING_LT) {
        if (frame_a == nullptr || frame_b == nullptr) return false;
        uint32_t start = (uint32_t)spec.start;
        uint32_t span = (uint32_t)spec.span;
        uint32_t dist = ast_hamming_distance(frame_a, frame_b, start, span, WORDS);
        return (uint64_t)dist < spec.threshold;
    }
    if (spec.kind == AST_PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT) {
        if (frame_a == nullptr || frame_b == nullptr) return false;
        uint32_t start = (uint32_t)spec.start;
        uint32_t span = (uint32_t)spec.span;
        uint32_t dist = ast_hamming_distance(frame_a, frame_b, start, span, WORDS);
        if ((uint64_t)dist >= spec.threshold) return false;
        uint32_t union_count = ast_union_popcount(frame_a, frame_b, start, span, WORDS);
        return (uint64_t)union_count < spec.threshold_b;
    }
    if (spec.kind == AST_PRED_KIND_HAMMING_LT_AND_EQ0 || spec.kind == AST_PRED_KIND_HAMMING_LT_AND_NE0) {
        if (frame_a == nullptr || frame_b == nullptr) return false;
        uint32_t start = (uint32_t)spec.start;
        uint32_t span = (uint32_t)spec.span;
        uint32_t dist = ast_hamming_distance(frame_a, frame_b, start, span, WORDS);
        if ((uint64_t)dist >= spec.threshold) return false;
        uint32_t idx = (uint32_t)spec.and_word;
        if (idx >= WORDS) return false;
        if (spec.kind == AST_PRED_KIND_HAMMING_LT_AND_EQ0) return frame_b[idx] == 0;
        return frame_b[idx] != 0;
    }
    if (spec.kind == AST_PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT_AND_EQ0 ||
        spec.kind == AST_PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT_AND_NE0) {
        if (frame_a == nullptr || frame_b == nullptr) return false;
        uint32_t start = (uint32_t)spec.start;
        uint32_t span = (uint32_t)spec.span;
        uint32_t dist = ast_hamming_distance(frame_a, frame_b, start, span, WORDS);
        if ((uint64_t)dist >= spec.threshold) return false;
        uint32_t union_count = ast_union_popcount(frame_a, frame_b, start, span, WORDS);
        if ((uint64_t)union_count >= spec.threshold_b) return false;
        uint32_t idx = (uint32_t)spec.and_word;
        if (idx >= WORDS) return false;
        if (spec.kind == AST_PRED_KIND_HAMMING_LT_AND_UNION_POPCNT_LT_AND_EQ0) return frame_b[idx] == 0;
        return frame_b[idx] != 0;
    }
    if (spec.kind == AST_PRED_KIND_POPCNT_LT_AND_EQ0 || spec.kind == AST_PRED_KIND_POPCNT_LT_AND_NE0) {
        if (frame_b == nullptr) return false;
        uint32_t pc = 0;
        uint32_t start = (uint32_t)spec.start;
        uint32_t span = (uint32_t)spec.span;
        for (uint32_t lane = 0; lane < span && start + lane < WORDS; lane++) {
            pc += (uint32_t)__popcll((unsigned long long)frame[start + lane]);
        }
        if ((uint64_t)pc >= spec.threshold) return false;
        uint32_t idx = (uint32_t)spec.and_word;
        if (idx >= WORDS) return false;
        if (spec.kind == AST_PRED_KIND_POPCNT_LT_AND_EQ0) return frame_b[idx] == 0;
        return frame_b[idx] != 0;
    }
    if (spec.kind != AST_PRED_KIND_POPCNT_LTE && spec.kind != AST_PRED_KIND_POPCNT_LT &&
        spec.kind != AST_PRED_KIND_POPCNT_GTE && spec.kind != AST_PRED_KIND_POPCNT_GT) return false;

    uint32_t count = 0;
    uint32_t start = (uint32_t)spec.start;
    uint32_t span = (uint32_t)spec.span;
    for (uint32_t lane = 0; lane < span && start + lane < WORDS; lane++) {
        count += (uint32_t)__popcll((unsigned long long)frame[start + lane]);
    }

    if (spec.kind == AST_PRED_KIND_POPCNT_LT) return (uint64_t)count < spec.threshold;
    if (spec.kind == AST_PRED_KIND_POPCNT_LTE) return (uint64_t)count <= spec.threshold;
    if (spec.kind == AST_PRED_KIND_POPCNT_GT) return (uint64_t)count > spec.threshold;
    return (uint64_t)count >= spec.threshold;
}

static __device__ __forceinline__ void ast_geometric(ast_decoded_instr instr, uint64_t* a_frame, uint64_t* b_frame, uint64_t final_res[64]) {
    uint64_t tmp[WORDS];
    #pragma unroll
    for (int lane = 0; lane < WORDS; lane++) tmp[lane] = 0;

    for (int lane = 0; lane < instr.a_span && lane < CONTEXT_WORDS; lane++) {
        int idx = instr.a_start + lane;
        if (idx < WORDS) tmp[CONTEXT_START_WORD + lane] = a_frame[idx];
    }
    if (b_frame != nullptr) {
        for (int lane = 0; lane < instr.b_span && lane < GRADIENT_WORDS; lane++) {
            int idx = instr.b_start + lane;
            if (idx < WORDS) tmp[GRADIENT_START_WORD + lane] = b_frame[idx];
        }
    }

    Multivector left = load_multivector(tmp, CONTEXT_START_WORD);
    Multivector right = load_multivector(tmp, GRADIENT_START_WORD);
    Multivector mv;
    if (instr.opcode == OPCODE_GEOMETRIC_COMPOSE) {
        mv = geometric_product(left, right);
    } else if (instr.opcode == OPCODE_GEOMETRIC_SANDWICH) {
        mv = sandwich(left, right);
    } else {
        mv = reverse(left);
    }
    for (int lane = 0; lane < SIGNALS_WORDS; lane++) {
        final_res[lane] = double_to_word(mv.v[lane]);
    }
}

static __device__ __forceinline__ int ast_payloads(ast_decoded_instr instr, ast_context ctx, uint64_t final_res[64]) {
    if (instr.a_ind == 1 && ctx.a != nullptr) instr.a_start = (int)(ctx.a[instr.a_start] & 0x7FULL);
    if (instr.b_type == AST_B_TYPE_INDIRECT && ctx.b != nullptr) instr.b_start = (int)(ctx.b[instr.b_start] & 0x7FULL);

    if (instr.mode == AST_MODE_GEOMETRIC || (instr.opcode & 0xF0ULL) != 0) {
        ast_geometric(instr, ctx.a, ctx.b, final_res);
        return instr.dst_span < SIGNALS_WORDS ? instr.dst_span : SIGNALS_WORDS;
    }

    uint64_t b_imm = (uint64_t)instr.b_start | ((uint64_t)(instr.b_span - 1) << 7);
    int lanes = instr.dst_span;
    if (instr.mode != AST_MODE_TRUTH && instr.mode != AST_MODE_EMIT && instr.mode != AST_MODE_EMIT_FROM_OWNER) {
        lanes = instr.a_span > instr.b_span ? instr.a_span : instr.b_span;
    }

    uint64_t truth[64];
    for (int lane = 0; lane < lanes; lane++) {
        uint64_t a = 0;
        uint64_t b = 0;
        int a_idx = instr.a_start + (lane % instr.a_span);
        if (ctx.a != nullptr && a_idx < WORDS) a = ctx.a[a_idx];
        if (instr.b_type == AST_B_TYPE_IMMEDIATE) {
            b = b_imm;
        } else {
            int b_idx = instr.b_start + (lane % instr.b_span);
            if (ctx.b != nullptr && b_idx < WORDS) b = ctx.b[b_idx];
        }
        truth[lane] = ast_truth_word(instr.opcode, a, b);
    }

    if (instr.mode == AST_MODE_POPCNT) {
        uint32_t total = 0;
        for (int lane = 0; lane < lanes; lane++) total += (uint32_t)__popcll((unsigned long long)truth[lane]);
        final_res[0] = total;
        return 1;
    }
    if (instr.mode == AST_MODE_ANY_ZERO) {
        uint64_t witness = 0;
        for (int lane = 0; lane < lanes; lane++) {
            if (truth[lane] != ~0ULL) witness = 1;
        }
        final_res[0] = witness;
        return 1;
    }
    if (instr.mode == AST_MODE_ALL_ONES) {
        uint64_t witness = 1;
        for (int lane = 0; lane < lanes; lane++) {
            if (truth[lane] != ~0ULL) witness = 0;
        }
        final_res[0] = witness;
        return 1;
    }
    if (instr.mode == AST_MODE_MIN_NONZERO) {
        uint64_t minimum = 0;
        for (int lane = 0; lane < lanes; lane++) {
            uint64_t candidate = truth[lane];
            if (candidate != 0 && (minimum == 0 || candidate < minimum)) {
                minimum = candidate;
            }
        }
        final_res[0] = minimum;
        return 1;
    }

    for (int lane = 0; lane < lanes; lane++) final_res[lane] = truth[lane];
    return lanes;
}

static __device__ __forceinline__ uint64_t ast_fold_combine(uint64_t mode, uint64_t opcode, uint64_t aggregate, uint64_t payload) {
    if (mode == AST_MODE_MIN_NONZERO) {
        if (aggregate == 0 || (payload != 0 && payload < aggregate)) {
            return payload;
        }
        return aggregate;
    }

    return ast_truth_word(opcode, aggregate, payload);
}

static __device__ __forceinline__ uint64_t ast_fold_payload(
    uint64_t* frames,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t owner_index,
    const predicate_device_spec_t* specs,
    uint32_t pc,
    uint64_t opcode,
    uint64_t mode,
    int dst_idx
) {
    bool seeded = false;
    uint64_t aggregate = 0;

    for (uint32_t source = 0; source < value_count; source++) {
        if (!ast_active(active, value_count, source)) continue;

        uint64_t* exec = owner_index == AST_INVALID_INDEX ? ast_frame(frames, source) : ast_frame(frames, owner_index);
        uint64_t raw = exec[PROGRAM_START_WORD + pc];
        if (raw == 0) continue;

        ast_decoded_instr instr = ast_decode(raw);
        if (instr.topology != AST_TOPOLOGY_FOLD || instr.opcode != opcode || instr.mode != mode || dst_idx < instr.dst_start || dst_idx >= instr.dst_start + instr.dst_span) continue;

        uint64_t* predicate = ast_frame(frames, source);
        ast_context ctx = ast_make_context(frames, active, value_count, owner_index, source, pc, raw, instr);
        uint64_t* pred_a = ctx.a;
        uint64_t* pred_b = ctx.b;
        if (owner_index != AST_INVALID_INDEX) {
            pred_a = ast_frame(frames, owner_index);
            pred_b = ast_frame(frames, source);
        }
        if (!ast_predicate_allows(predicate, instr, specs, pred_a, pred_b)) continue;

        uint64_t final_res[64];
        int final_len = ast_payloads(instr, ctx, final_res);
        uint64_t payload = final_len == 1 ? final_res[0] : final_res[dst_idx - instr.dst_start];

        if (!seeded) {
            aggregate = payload;
            seeded = true;
        } else {
            aggregate = ast_fold_combine(mode, opcode, aggregate, payload);
        }
    }

    return aggregate;
}

__global__ void hypercube_ast_kernel(
    uint64_t* frames,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t owner_index,
    const predicate_device_spec_t* specs,
    uint64_t* post,
    uint64_t* spawn_frames,
    const uint64_t* spawn_ids,
    uint8_t* spawn_active
) {
    uint32_t lid = threadIdx.x;
    if (lid >= value_count) return;

    bool target_active = active[lid] != 0;
    uint64_t* target_frame = ast_frame(frames, lid);
    uint64_t* target_post = post + (uint64_t)lid * (uint64_t)WORDS;

    for (uint32_t pc = 0; pc < PROGRAM_WORDS; pc++) {
        for (uint32_t word = 0; word < WORDS; word++) {
            target_post[word] = target_active ? target_frame[word] : 0;
        }

        __syncthreads();

        if (target_active) {
            for (uint32_t source = 0; source < value_count; source++) {
                if (!ast_active(active, value_count, source)) continue;

                uint64_t* exec = owner_index == AST_INVALID_INDEX ? ast_frame(frames, source) : ast_frame(frames, owner_index);
                uint64_t raw = exec[PROGRAM_START_WORD + pc];
                if (raw == 0) continue;

                ast_decoded_instr instr = ast_decode(raw);
                if (instr.topology == AST_TOPOLOGY_SPAWN) continue;

                uint64_t* predicate = ast_frame(frames, source);
                ast_context ctx = ast_make_context(frames, active, value_count, owner_index, source, pc, raw, instr);
                uint64_t* pred_a = ctx.a;
                uint64_t* pred_b = ctx.b;
                if (owner_index != AST_INVALID_INDEX) {
                    pred_a = ast_frame(frames, owner_index);
                    pred_b = ast_frame(frames, source);
                }
                if (!ast_predicate_allows(predicate, instr, specs, pred_a, pred_b)) continue;

                int route = ast_route(source, value_count, owner_index, pc, instr, raw);
                if (route != (int)lid) continue;

                uint64_t final_res[64];
                int final_len = ast_payloads(instr, ctx, final_res);
                for (int lane = 0; lane < instr.dst_span; lane++) {
                    int dst = instr.dst_start + lane;
                    if (dst >= WORDS) continue;

                    uint64_t payload = final_len == 1 ? final_res[0] : (lane < final_len ? final_res[lane] : 0);
                    if (instr.topology == AST_TOPOLOGY_FOLD) {
                        payload = ast_fold_payload(frames, active, value_count, owner_index, specs, pc, instr.opcode, instr.mode, dst);
                    }
                    target_post[dst] = payload;
                }
            }
        }

        if (target_active && spawn_frames != nullptr && spawn_ids[lid] != 0) {
            uint64_t* source = target_frame;
            uint64_t* exec = owner_index == AST_INVALID_INDEX ? source : ast_frame(frames, owner_index);
            uint64_t raw = exec[PROGRAM_START_WORD + pc];
            if (raw != 0) {
                ast_decoded_instr instr = ast_decode(raw);
                // ModeEmitFromOwner collapses per-source fan-out so a single program
                // tick yields one child instead of one per community member.
                if (instr.mode == AST_MODE_EMIT_FROM_OWNER && (uint32_t)lid != owner_index) {
                    instr.topology = AST_TOPOLOGY_SELF;
                }
                if (instr.topology == AST_TOPOLOGY_SPAWN) {
                    ast_context ctx = ast_make_context(frames, active, value_count, owner_index, lid, pc, raw, instr);
                    uint64_t* pred_a = ctx.a;
                    uint64_t* pred_b = ctx.b;
                    if (owner_index != AST_INVALID_INDEX) {
                        pred_a = ast_frame(frames, owner_index);
                        pred_b = ast_frame(frames, lid);
                    }
                    if (ast_predicate_allows(source, instr, specs, pred_a, pred_b)) {
                        uint64_t* spawned = spawn_frames + (uint64_t)lid * (uint64_t)WORDS;
                        if (spawn_active[lid] == 0) {
                            for (uint32_t word = 0; word < WORDS; word++) spawned[word] = source[word];
                            spawned[ID_START_WORD] = spawn_ids[lid];
                            for (uint32_t word = 0; word < PROGRAM_WORDS; word++) spawned[PROGRAM_START_WORD + word] = 0;
                            spawned[SCHEDULING_NEXT_PROGRAM_WORD] = 0;
                            spawned[PROPERTIES_STATUS_WORD] = STATUS_PENDING;
                            spawn_active[lid] = 1;
                        }

                        uint64_t final_res[64];
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

        __syncthreads();

        if (target_active) {
            for (uint32_t word = 0; word < WORDS; word++) {
                target_frame[word] = target_post[word];
            }
        }

        __syncthreads();
    }
}

static uint64_t* d_pool_A = nullptr;
static uint32_t pool_capacity = 0;

static int ensure_pool(uint32_t num_values) {
    if (pool_capacity >= num_values) return 0;

    if (d_pool_A) { cudaFree(d_pool_A); d_pool_A = nullptr; }

    uint32_t cap = num_values * 2;
    if (cap < 1024) cap = 1024;

    size_t bytes = (size_t)cap * WORDS * sizeof(uint64_t);
    if (cudaMalloc((void**)&d_pool_A, bytes) != cudaSuccess) return -1;

    pool_capacity = cap;
    return 0;
}

// Pinned arena state for the gossip kernel.
// CUDA does not have Apple Silicon's unified shared memory; we allocate
// a device-resident arena and stream Value frames in/out per dispatch.
// gossip_arena_cap counts Value frames (not bytes) of headroom.
static uint64_t* d_gossip_arena   = nullptr;
static uint8_t*  d_gossip_active  = nullptr;
static uint64_t* d_ast_post       = nullptr;
static predicate_device_spec_t* d_ast_predicates = nullptr;
static uint64_t* d_spawn_frames   = nullptr;
static uint64_t* d_spawn_ids      = nullptr;
static uint8_t*  d_spawn_active   = nullptr;
static uint32_t  gossip_arena_cap = 0;
static uint32_t  gossip_active_cap = 0;
static uint32_t  ast_post_cap     = 0;
static uint32_t  spawn_cap        = 0;

static int ensure_gossip_pool(uint32_t value_count) {
    if (value_count > gossip_arena_cap) {
        if (d_gossip_arena) { cudaFree(d_gossip_arena); d_gossip_arena = nullptr; }
        uint32_t cap = value_count * 2;
        if (cap < 256) cap = 256;
        size_t bytes = (size_t)cap * WORDS * sizeof(uint64_t);
        if (cudaMalloc((void**)&d_gossip_arena, bytes) != cudaSuccess) return -1;
        gossip_arena_cap = cap;
    }
    if (value_count > gossip_active_cap) {
        if (d_gossip_active) { cudaFree(d_gossip_active); d_gossip_active = nullptr; }
        uint32_t cap = value_count * 2;
        if (cap < 256) cap = 256;
        if (cudaMalloc((void**)&d_gossip_active, (size_t)cap * sizeof(uint8_t)) != cudaSuccess) return -1;
        gossip_active_cap = cap;
    }
    if (value_count > ast_post_cap) {
        if (d_ast_post) { cudaFree(d_ast_post); d_ast_post = nullptr; }
        uint32_t cap = value_count * 2;
        if (cap < 256) cap = 256;
        if (cudaMalloc((void**)&d_ast_post, (size_t)cap * WORDS * sizeof(uint64_t)) != cudaSuccess) return -1;
        ast_post_cap = cap;
    }
    if (!d_ast_predicates) {
        if (cudaMalloc((void**)&d_ast_predicates, 128 * sizeof(predicate_device_spec_t)) != cudaSuccess) return -1;
    }
    if (value_count > spawn_cap) {
        if (d_spawn_frames) { cudaFree(d_spawn_frames); d_spawn_frames = nullptr; }
        if (d_spawn_ids) { cudaFree(d_spawn_ids); d_spawn_ids = nullptr; }
        if (d_spawn_active) { cudaFree(d_spawn_active); d_spawn_active = nullptr; }
        uint32_t cap = value_count * 2;
        if (cap < 256) cap = 256;
        if (cudaMalloc((void**)&d_spawn_frames, (size_t)cap * WORDS * sizeof(uint64_t)) != cudaSuccess) return -1;
        if (cudaMalloc((void**)&d_spawn_ids, (size_t)cap * sizeof(uint64_t)) != cudaSuccess) return -1;
        if (cudaMalloc((void**)&d_spawn_active, (size_t)cap * sizeof(uint8_t)) != cudaSuccess) return -1;
        spawn_cap = cap;
    }
    return 0;
}

/*
hypercube_gossip_v2_kernel mirrors pkg/compute/kernel/cpu/hypercube_gossip_go_kernel.go
and pkg/compute/kernel/metal/backend.metal:hypercube_gossip_kernel. The
new ALU's instruction format is decoded inline, supports the four
topologies (Local, Pop, Hypercube, HypercubePerPeer), per-peer
predicate evaluation under HypercubePerPeer, per-peer mask gating in
the broadcast write, and per-peer stage selection. spawned children
are surfaced via the spawn-register word; the host post-processes that
into spawn_frames / spawn_ids the same way the CPU and Metal paths do.

Keep this kernel byte-compatible with the Go reference: the kernel-host
contract for stage_indices / stage_count / spawn_register is identical
across substrates so the backend selector can fall over freely.
*/

#define HG_V2_TOPO_LOCAL              0u
#define HG_V2_TOPO_POP                1u
#define HG_V2_TOPO_HYPERCUBE          2u
#define HG_V2_TOPO_HYPERCUBE_PER_PEER 3u

#define HG_V2_PRED_LT             0u
#define HG_V2_PRED_LE             1u
#define HG_V2_PRED_GT             2u
#define HG_V2_PRED_GE             3u
#define HG_V2_PRED_EQ             4u
#define HG_V2_PRED_NE             5u
#define HG_V2_PRED_STORE_POPCNT   6u
#define HG_V2_PRED_ANY_ZERO       7u
#define HG_V2_REDUCE_ARGMIN       1ull
#define HG_V2_REDUCE_MODE_EQ      2ull
#define HG_V2_REDUCE_ZIPF_SELECT  3ull
#define HG_V2_MODE_TABLE_SIZE     1024u
#define HG_V2_ZIPF_WEIGHT_SCALE   281474976710656ull
#define HG_V2_ZIPF_ID_WORD        122u
#define HG_V2_ZIPF_EPOCH_WORD     58u
#define HG_V2_ZIPF_COMMUNITY_WORD 64u
#define HG_V2_ZIPF_SURPRISAL_WORD 68u
#define HG_V2_SPAWN_REGISTER_WORD   70u
#define HG_V2_ZIPF_MAX_CANDS        1024u

static __device__ __forceinline__ uint64_t hg_v2_rotated_word(
    uint64_t* frame, uint32_t start, uint32_t span, uint32_t lane, uint32_t rotate
) {
    uint32_t idx = lane % span;
    uint64_t word = frame[(start + idx) & 127u];
    if (rotate == 0u) return word;
    uint32_t shift = rotate * 8u;
    uint64_t next = frame[(start + ((idx + 1u) % span)) & 127u];
    return (word >> shift) | (next << (64u - shift));
}

static __device__ __forceinline__ uint64_t hg_v2_popcount_span(
    uint64_t* frame, uint32_t start, uint32_t span
) {
    uint64_t total = 0;
    for (uint32_t lane = 0; lane < span; lane++) {
        total += (uint64_t)__popcll(frame[(start + lane) & 127u]);
    }
    return total;
}

static __device__ __forceinline__ void hg_v2_reduce_argmin(
    uint64_t* owner,
    uint64_t* arena,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t value_start,
    uint32_t key_start,
    uint32_t dst_start,
    uint32_t guard_start
) {
    if (owner == nullptr || value_count == 0u || owner[guard_start & 127u] == 0ull) return;

    uint64_t best_value = 0ull;
    uint64_t best_key = ~0ull;

    for (uint32_t idx = 0; idx < value_count; idx++) {
        if (active[idx] == 0) continue;

        uint64_t* peer = arena + (size_t)idx * WORDS;
        uint64_t value = peer[value_start & 127u];
        if (value == 0ull) continue;

        uint64_t key = peer[key_start & 127u];
        if (key >= best_key) continue;

        best_key = key;
        best_value = value;
    }

    if (best_value != 0ull) owner[dst_start & 127u] = best_value;
}

static __device__ __forceinline__ void hg_v2_reduce_mode_eq(
    uint64_t* owner,
    uint64_t* arena,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t value_start,
    uint32_t key_start,
    uint32_t dst_start,
    uint32_t match_start,
    uint8_t* used,
    uint64_t* keys,
    uint32_t* counts,
    uint32_t* first_seen
) {
    if (owner == nullptr || value_count == 0u) return;

    uint64_t match = owner[match_start & 127u];
    if (match == 0ull) return;

    for (uint32_t slot = 0; slot < HG_V2_MODE_TABLE_SIZE; slot++) {
        used[slot] = 0u;
        keys[slot] = 0ull;
        counts[slot] = 0u;
        first_seen[slot] = 0xFFFFFFFFu;
    }

    uint32_t seen = 0u;
    for (uint32_t idx = 0; idx < value_count; idx++) {
        if (active[idx] == 0) continue;

        uint64_t* peer = arena + (size_t)idx * WORDS;
        if (peer[key_start & 127u] != match) continue;

        uint64_t value = peer[value_start & 127u];
        if (value == 0ull) continue;

        uint32_t slot = (uint32_t)((value ^ (value >> 32ull)) & (uint64_t)(HG_V2_MODE_TABLE_SIZE - 1u));
        for (uint32_t probe = 0u; probe < HG_V2_MODE_TABLE_SIZE; probe++) {
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

            slot = (slot + 1u) & (HG_V2_MODE_TABLE_SIZE - 1u);
        }

        seen++;
    }

    uint64_t best_value = 0ull;
    uint32_t best_count = 0u;
    uint32_t best_order = 0xFFFFFFFFu;

    for (uint32_t slot = 0; slot < HG_V2_MODE_TABLE_SIZE; slot++) {
        if (used[slot] == 0u) continue;
        if (counts[slot] < best_count) continue;
        if (counts[slot] == best_count && first_seen[slot] >= best_order) continue;

        best_count = counts[slot];
        best_order = first_seen[slot];
        best_value = keys[slot];
    }

    if (best_value != 0ull) owner[dst_start & 127u] = best_value;
}

static __device__ __forceinline__ uint32_t hg_v2_zipf_power(uint64_t temperature) {
    if (temperature >= 1024ull) return 0u;
    if (temperature >= 512ull) return 1u;
    if (temperature >= 256ull) return 2u;
    if (temperature >= 128ull) return 3u;
    return 4u;
}

static __device__ __forceinline__ uint64_t hg_v2_zipf_weight(uint32_t rank, uint32_t power) {
    if (rank == 0u) return 0ull;
    if (power == 0u) return 1ull;

    uint64_t weight = HG_V2_ZIPF_WEIGHT_SCALE;
    uint64_t divisor = (uint64_t)rank;

    for (uint32_t idx = 0u; idx < power; idx++) {
        weight /= divisor;
        if (weight == 0ull) return 1ull;
    }

    return weight == 0ull ? 1ull : weight;
}

static __device__ __forceinline__ uint64_t hg_v2_zipf_rotl(uint64_t value, uint32_t shift) {
    shift &= 63u;

    uint32_t rshift = (64u - shift) & 63u;

    return (value << shift) | (value >> rshift);
}

static __device__ __forceinline__ uint64_t hg_v2_zipf_mix(uint64_t seed) {
    seed += 0x9e3779b97f4a7c15ull;
    seed = (seed ^ (seed >> 30u)) * 0xbf58476d1ce4e5b9ull;
    seed = (seed ^ (seed >> 27u)) * 0x94d049bb133111ebull;
    return seed ^ (seed >> 31u);
}

static __device__ __forceinline__ uint64_t hg_v2_zipf_seed(
    uint64_t* owner, uint32_t count, uint64_t best_utility
) {
    uint64_t seed = owner[HG_V2_ZIPF_ID_WORD] ^
        hg_v2_zipf_rotl(owner[HG_V2_ZIPF_EPOCH_WORD], 17u) ^
        hg_v2_zipf_rotl(owner[HG_V2_ZIPF_COMMUNITY_WORD], 31u) ^
        hg_v2_zipf_rotl(owner[HG_V2_ZIPF_SURPRISAL_WORD], 7u) ^
        hg_v2_zipf_rotl(best_utility, 43u) ^
        (uint64_t)count;

    return hg_v2_zipf_mix(seed);
}

static __device__ __forceinline__ uint32_t hg_v2_zipf_candidate_count(
    uint64_t* arena, const uint8_t* active, uint32_t value_count, uint32_t value_start
) {
    uint32_t count = 0u;

    for (uint32_t idx = 0u; idx < value_count; idx++) {
        if (active[idx] == 0) continue;

        uint64_t* peer = arena + (size_t)idx * WORDS;
        if (peer[value_start & 127u] == 0ull) continue;

        count++;
    }

    return count;
}

static __device__ __forceinline__ bool hg_v2_zipf_best(
    uint64_t* arena,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t value_start,
    uint32_t utility_start,
    uint64_t* best_value,
    uint64_t* best_utility
) {
    uint32_t best_index = 0u;
    bool found = false;
    *best_value = 0ull;
    *best_utility = 0ull;

    for (uint32_t idx = 0u; idx < value_count; idx++) {
        if (active[idx] == 0) continue;

        uint64_t* peer = arena + (size_t)idx * WORDS;
        uint64_t value = peer[value_start & 127u];
        if (value == 0ull) continue;

        uint64_t utility = peer[utility_start & 127u];
        if (found && (utility < *best_utility || (utility == *best_utility && idx >= best_index))) continue;

        *best_value = value;
        *best_utility = utility;
        best_index = idx;
        found = true;
    }

    return found;
}

static __device__ __forceinline__ uint64_t hg_v2_zipf_candidate_at_rank(
    uint64_t* arena,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t value_start,
    uint32_t utility_start,
    uint32_t target_rank,
    uint32_t* cand_idx,
    uint64_t* cand_util,
    uint64_t* cand_val
) {
    uint32_t cand_count = 0u;

    for (uint32_t idx = 0u; idx < value_count; idx++) {
        if (active[idx] == 0) continue;

        uint64_t* peer = arena + (size_t)idx * WORDS;
        uint64_t value = peer[value_start & 127u];
        if (value == 0ull) continue;

        uint64_t utility = peer[utility_start & 127u];

        if (cand_count >= HG_V2_ZIPF_MAX_CANDS) continue;

        cand_idx[cand_count] = idx;
        cand_util[cand_count] = utility;
        cand_val[cand_count] = value;
        cand_count++;
    }

    if (target_rank == 0u || target_rank > cand_count) return 0ull;

    for (uint32_t sorted_i = 1u; sorted_i < cand_count; sorted_i++) {
        uint32_t ci = cand_idx[sorted_i];
        uint64_t cu = cand_util[sorted_i];
        uint64_t cv = cand_val[sorted_i];
        uint32_t j = sorted_i;

        while (j > 0u) {
            uint32_t pj = cand_idx[j - 1u];
            uint64_t pu = cand_util[j - 1u];

            if (pu > cu || (pu == cu && pj < ci)) break;

            cand_idx[j] = cand_idx[j - 1u];
            cand_util[j] = cand_util[j - 1u];
            cand_val[j] = cand_val[j - 1u];
            j--;
        }

        cand_idx[j] = ci;
        cand_util[j] = cu;
        cand_val[j] = cv;
    }

    return cand_val[target_rank - 1u];
}

static __device__ __forceinline__ void hg_v2_reduce_zipf_select(
    uint64_t* owner,
    uint64_t* arena,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t value_start,
    uint32_t utility_start,
    uint32_t dst_start,
    uint32_t temperature_start,
    uint32_t* zipf_idx,
    uint64_t* zipf_util,
    uint64_t* zipf_val
) {
    if (owner == nullptr || value_count == 0u) return;

    uint32_t count = hg_v2_zipf_candidate_count(arena, active, value_count, value_start);
    if (count == 0u) return;

    uint64_t best_value = 0ull;
    uint64_t best_utility = 0ull;
    if (!hg_v2_zipf_best(arena, active, value_count, value_start, utility_start, &best_value, &best_utility)) return;

    uint64_t temperature = owner[temperature_start & 127u];
    if (temperature == 0ull || count == 1u) {
        owner[dst_start & 127u] = best_value;
        return;
    }

    uint32_t power = hg_v2_zipf_power(temperature);
    uint64_t total = 0ull;
    for (uint32_t rank = 1u; rank <= count; rank++) {
        total += hg_v2_zipf_weight(rank, power);
    }

    if (total == 0ull) {
        owner[dst_start & 127u] = best_value;
        return;
    }

    uint64_t ticket = hg_v2_zipf_seed(owner, count, best_utility) % total;
    uint64_t running = 0ull;

    for (uint32_t rank = 1u; rank <= count; rank++) {
        running += hg_v2_zipf_weight(rank, power);
        if (ticket >= running) continue;

        uint64_t selected = hg_v2_zipf_candidate_at_rank(
            arena,
            active,
            value_count,
            value_start,
            utility_start,
            rank,
            zipf_idx,
            zipf_util,
            zipf_val
        );
        owner[dst_start & 127u] = selected == 0ull ? best_value : selected;
        return;
    }

    uint64_t selected = hg_v2_zipf_candidate_at_rank(
        arena,
        active,
        value_count,
        value_start,
        utility_start,
        count,
        zipf_idx,
        zipf_util,
        zipf_val
    );
    owner[dst_start & 127u] = selected == 0ull ? best_value : selected;
}

__global__ void hypercube_gossip_v2_kernel(
    uint64_t* arena,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t owner_index,
    uint32_t* stage_indices,
    uint32_t* stage_count
) {
    if (threadIdx.x != 0 || blockIdx.x != 0) return;

    uint64_t* owner = arena + (size_t)owner_index * WORDS;
    if (owner_index == 0xFFFFFFFFu) {
        // Sentinel owner: the orchestrator passes the owner separately from
        // the community arena. We expect arena slot 0 to hold the owner in
        // that case but the production wrapper always loads the owner
        // explicitly, so this branch is only a safety pin for malformed
        // launches.
        return;
    }

    if (stage_count != nullptr) stage_count[0] = 0u;

    __shared__ uint8_t s_mode_used[HG_V2_MODE_TABLE_SIZE];
    __shared__ uint64_t s_mode_keys[HG_V2_MODE_TABLE_SIZE];
    __shared__ uint32_t s_mode_counts[HG_V2_MODE_TABLE_SIZE];
    __shared__ uint32_t s_mode_first_seen[HG_V2_MODE_TABLE_SIZE];
    __shared__ uint32_t s_zipf_idx[HG_V2_ZIPF_MAX_CANDS];
    __shared__ uint64_t s_zipf_util[HG_V2_ZIPF_MAX_CANDS];
    __shared__ uint64_t s_zipf_val[HG_V2_ZIPF_MAX_CANDS];

    uint32_t b_queue_idx = 0u;
    uint32_t current_b_idx = 0u;
    uint64_t* current_b = nullptr;
    uint32_t pop_body_start = 0u;
    bool pop_active = false;

    for (uint32_t pc = 0; pc < PROGRAM_WORDS; pc++) {
        uint64_t raw = owner[PROGRAM_START_WORD + pc];
        if (raw == 0ull) continue;

        uint64_t opcode = raw & 0xFull;
        uint32_t a_start  = (uint32_t)((raw >> 4ull)  & 0x7Full);
        uint32_t a_span   = (uint32_t)(((raw >> 11ull) & 0x7Full) + 1ull);
        uint32_t b_start  = (uint32_t)((raw >> 18ull) & 0x7Full);
        uint32_t b_span   = (uint32_t)(((raw >> 25ull) & 0x7Full) + 1ull);
        uint32_t dst_start = (uint32_t)((raw >> 32ull) & 0x7Full);
        uint32_t dst_span  = (uint32_t)(((raw >> 39ull) & 0x7Full) + 1ull);
        uint32_t mask_start = (uint32_t)((raw >> 46ull) & 0x7Full);

        uint64_t target_b   = (raw >> 53ull) & 1ull;
        uint64_t emit_flag  = (raw >> 54ull) & 1ull;
        uint32_t topology   = (uint32_t)((raw >> 55ull) & 3ull);
        uint64_t predicate  = (raw >> 57ull) & 1ull;
        uint64_t pred_cond  = (raw >> 58ull) & 7ull;
        uint64_t b_rotate   = pred_cond;
        uint64_t src_a_from_b = (raw >> 61ull) & 1ull;
        uint64_t stage_bit  = (raw >> 62ull) & 1ull;
        uint64_t pop_end    = (raw >> 63ull) & 1ull;

        if (predicate == 1ull) {
            if (opcode == HG_V2_REDUCE_ARGMIN) {
                hg_v2_reduce_argmin(owner, arena, active, value_count, a_start, b_start, dst_start, mask_start);
                continue;
            }

            if (opcode == HG_V2_REDUCE_MODE_EQ) {
                hg_v2_reduce_mode_eq(
                    owner,
                    arena,
                    active,
                    value_count,
                    a_start,
                    b_start,
                    dst_start,
                    mask_start,
                    s_mode_used,
                    s_mode_keys,
                    s_mode_counts,
                    s_mode_first_seen
                );
                continue;
            }

            if (opcode == HG_V2_REDUCE_ZIPF_SELECT) {
                hg_v2_reduce_zipf_select(
                    owner,
                    arena,
                    active,
                    value_count,
                    a_start,
                    b_start,
                    dst_start,
                    mask_start,
                    s_zipf_idx,
                    s_zipf_util,
                    s_zipf_val
                );
                continue;
            }
        }

        if (topology == HG_V2_TOPO_POP && b_queue_idx < value_count) {
            // Find next active community frame.
            while (b_queue_idx < value_count && active[b_queue_idx] == 0) b_queue_idx++;
            if (b_queue_idx < value_count) {
                current_b = arena + (size_t)b_queue_idx * WORDS;
                current_b_idx = b_queue_idx;
                b_queue_idx++;
                pop_body_start = pc + 1u;
                pop_active = true;
            }
        }

        if (stage_bit == 1ull) {
            if (topology == HG_V2_TOPO_HYPERCUBE_PER_PEER && value_count > 0u && stage_indices != nullptr && stage_count != nullptr) {
                for (uint32_t k = 0; k < value_count; k++) {
                    if (k == owner_index || active[k] == 0) continue;
                    uint64_t* peer = arena + (size_t)k * WORDS;
                    if (peer[mask_start & 127u] == 0ull) continue;
                    uint32_t out_idx = stage_count[0];
                    if (out_idx >= value_count) break;
                    stage_indices[out_idx] = k;
                    stage_count[0] = out_idx + 1u;
                }
            } else if (current_b != nullptr && stage_indices != nullptr && stage_count != nullptr) {
                uint32_t out_idx = stage_count[0];
                if (out_idx < value_count) {
                    stage_indices[out_idx] = current_b_idx;
                    stage_count[0] = out_idx + 1u;
                }
            }

            if (pop_end == 1ull && pop_active) {
                while (b_queue_idx < value_count && active[b_queue_idx] == 0) b_queue_idx++;
                if (b_queue_idx < value_count) {
                    current_b = arena + (size_t)b_queue_idx * WORDS;
                    current_b_idx = b_queue_idx;
                    b_queue_idx++;
                    pc = pop_body_start - 1u;
                    continue;
                }
                pop_active = false;
            }

            continue;
        }

        // Per-peer predicate (TopoHypercubePerPeer): evaluate per peer,
        // write per-peer mask into peer[dst_start].
        if (predicate == 1ull && topology == HG_V2_TOPO_HYPERCUBE_PER_PEER && value_count > 0u) {
            uint64_t threshold = owner[b_start & 127u];
            for (uint32_t k = 0; k < value_count; k++) {
                if (k == owner_index || active[k] == 0) continue;
                uint64_t* peer = arena + (size_t)k * WORDS;
                uint64_t* witness_src = (src_a_from_b == 1ull) ? peer : owner;
                uint64_t per_pop = hg_v2_popcount_span(witness_src, a_start, a_span);
                uint64_t witness = (a_span == 1u) ? witness_src[a_start & 127u] : per_pop;

                bool hit = false;
                switch (pred_cond) {
                    case HG_V2_PRED_LT: hit = witness <  threshold; break;
                    case HG_V2_PRED_LE: hit = witness <= threshold; break;
                    case HG_V2_PRED_GT: hit = witness >  threshold; break;
                    case HG_V2_PRED_GE: hit = witness >= threshold; break;
                    case HG_V2_PRED_EQ: hit = witness == threshold; break;
                    case HG_V2_PRED_NE: hit = witness != threshold; break;
                    default: hit = false; break;
                }

                peer[dst_start & 127u] = hit ? ~0ull : 0ull;
            }
            continue;
        }

        // Pointer setup for single-peer / owner-side paths.
        uint64_t* ptr_b = current_b;
        if (topology == HG_V2_TOPO_HYPERCUBE && value_count > 0u && owner_index != 0xFFFFFFFFu) {
            uint32_t peer_idx = owner_index ^ 1u;
            if (peer_idx < value_count && active[peer_idx] != 0) {
                ptr_b = arena + (size_t)peer_idx * WORDS;
            }
        }
        if (ptr_b == nullptr) ptr_b = owner;

        uint64_t* ptr_a = (src_a_from_b == 1ull) ? ptr_b : owner;
        uint64_t* ptr_dst = (target_b == 1ull) ? ptr_b : owner;

        if (predicate == 1ull) {
            uint64_t guard = owner[mask_start & 127u];
            uint64_t pop = hg_v2_popcount_span(ptr_a, a_start, a_span);

            if (pred_cond == HG_V2_PRED_STORE_POPCNT) {
                uint32_t dst_idx = dst_start & 127u;
                uint64_t prev_dst = ptr_dst[dst_idx];
                ptr_dst[dst_idx] = (pop & guard) | (prev_dst & ~guard);
            } else if (pred_cond == HG_V2_PRED_ANY_ZERO) {
                bool zero_seen = false;
                for (uint32_t lane = 0; lane < a_span; lane++) {
                    if (ptr_a[(a_start + lane) & 127u] == 0ull) { zero_seen = true; break; }
                }
                uint64_t result = zero_seen ? ~0ull : 0ull;
                uint32_t dst_idx = dst_start & 127u;
                uint64_t prev_dst = ptr_dst[dst_idx];
                ptr_dst[dst_idx] = (result & guard) | (prev_dst & ~guard);
            } else {
                uint64_t threshold = owner[b_start & 127u];
                uint64_t witness = (a_span == 1u) ? ptr_a[a_start & 127u] : pop;
                bool hit = false;
                switch (pred_cond) {
                    case HG_V2_PRED_LT: hit = witness <  threshold; break;
                    case HG_V2_PRED_LE: hit = witness <= threshold; break;
                    case HG_V2_PRED_GT: hit = witness >  threshold; break;
                    case HG_V2_PRED_GE: hit = witness >= threshold; break;
                    case HG_V2_PRED_EQ: hit = witness == threshold; break;
                    case HG_V2_PRED_NE: hit = witness != threshold; break;
                    default: hit = false; break;
                }
                ptr_dst[dst_start & 127u] = (hit ? ~0ull : 0ull) & guard;
            }

            if (pop_end == 1ull && pop_active) {
                while (b_queue_idx < value_count && active[b_queue_idx] == 0) b_queue_idx++;
                if (b_queue_idx < value_count) {
                    current_b = arena + (size_t)b_queue_idx * WORDS;
                    current_b_idx = b_queue_idx;
                    b_queue_idx++;
                    pc = pop_body_start - 1u;
                    continue;
                }
                pop_active = false;
            }

            continue;
        }

        // Truth-table broadcast.
        uint64_t mask = owner[mask_start & 127u];
        uint64_t m0 = ((opcode >> 0) & 1ull) ? ~0ull : 0ull;
        uint64_t m1 = ((opcode >> 1) & 1ull) ? ~0ull : 0ull;
        uint64_t m2 = ((opcode >> 2) & 1ull) ? ~0ull : 0ull;
        uint64_t m3 = ((opcode >> 3) & 1ull) ? ~0ull : 0ull;

        bool hypercube = (topology == HG_V2_TOPO_HYPERCUBE || topology == HG_V2_TOPO_HYPERCUBE_PER_PEER) && value_count > 0u;
        bool per_peer_mask = topology == HG_V2_TOPO_HYPERCUBE_PER_PEER;

        if (hypercube && target_b == 1ull) {
            for (uint32_t k = 0; k < value_count; k++) {
                if (k == owner_index || active[k] == 0) continue;
                uint64_t* peer = arena + (size_t)k * WORDS;
                uint64_t peer_mask = per_peer_mask ? peer[mask_start & 127u] : mask;

                for (uint32_t lane = 0; lane < dst_span; lane++) {
                    uint64_t word_a = ptr_a[(a_start + (lane % a_span)) & 127u];
                    uint64_t word_b = hg_v2_rotated_word(peer, b_start, b_span, lane, (uint32_t)b_rotate);
                    uint64_t res = (word_a & word_b & m0)
                                 | (word_a & ~word_b & m1)
                                 | (~word_a & word_b & m2)
                                 | (~word_a & ~word_b & m3);
                    uint32_t dst_idx = (dst_start + lane) & 127u;
                    uint64_t prev_dst = peer[dst_idx];
                    peer[dst_idx] = (res & peer_mask) | (prev_dst & ~peer_mask);
                }
            }
        } else {
            uint32_t peers = hypercube ? value_count : 1u;
            for (uint32_t lane = 0; lane < dst_span; lane++) {
                uint64_t start_a = ptr_a[(a_start + (lane % a_span)) & 127u];
                uint32_t dst_idx = (dst_start + lane) & 127u;
                uint64_t prev_dst = ptr_dst[dst_idx];
                uint64_t acc = start_a;
                bool any = false;

                for (uint32_t k = 0; k < peers; k++) {
                    uint64_t* peer = ptr_b;
                    if (hypercube) {
                        if (k == owner_index || active[k] == 0) continue;
                        peer = arena + (size_t)k * WORDS;
                    }

                    uint64_t word_b = hg_v2_rotated_word(peer, b_start, b_span, lane, (uint32_t)b_rotate);
                    acc = (acc & word_b & m0)
                        | (acc & ~word_b & m1)
                        | (~acc & word_b & m2)
                        | (~acc & ~word_b & m3);
                    any = true;
                }

                if (!any) acc = start_a;
                ptr_dst[dst_idx] = (acc & mask) | (prev_dst & ~mask);
            }
        }

        if (emit_flag == 1ull && mask != 0ull) {
            owner[HG_V2_SPAWN_REGISTER_WORD]++;
        }
    }
}

extern "C" {

    int cuda_device_count() {
        int count = 0;
        if (cudaGetDeviceCount(&count) != cudaSuccess) return 0;
        return count;
    }

    void cleanup_cuda_pools() {
        if (d_pool_A)          { cudaFree(d_pool_A);          d_pool_A          = nullptr; }
        if (d_gossip_arena)    { cudaFree(d_gossip_arena);    d_gossip_arena    = nullptr; }
        if (d_gossip_active)   { cudaFree(d_gossip_active);   d_gossip_active   = nullptr; }
        if (d_ast_post)        { cudaFree(d_ast_post);        d_ast_post        = nullptr; }
        if (d_ast_predicates)  { cudaFree(d_ast_predicates);  d_ast_predicates  = nullptr; }
        if (d_spawn_frames)    { cudaFree(d_spawn_frames);    d_spawn_frames    = nullptr; }
        if (d_spawn_ids)       { cudaFree(d_spawn_ids);       d_spawn_ids       = nullptr; }
        if (d_spawn_active)    { cudaFree(d_spawn_active);    d_spawn_active    = nullptr; }
        pool_capacity   = 0;
        gossip_arena_cap = 0;
        gossip_active_cap = 0;
        ast_post_cap = 0;
        spawn_cap = 0;
    }

    /*
    hypercube_gossip_cuda runs the resident packed AST over a contiguous
    community frame array. active preserves nil community positions so
    topology routing matches the caller's slice indices. spawn_* buffers are
    value_count entries; the kernel fills at most one spawned frame per source.
    */
    int hypercube_gossip_cuda(
        int                         device_id,
        uint64_t*                   value_frames_host,
        uint8_t*                    active_host,
        uint32_t                    value_count,
        uint32_t                    owner_index,
        predicate_device_spec_t*    predicates_host,
        uint64_t*                   spawn_frames_host,
        uint64_t*                   spawn_ids_host,
        uint8_t*                    spawn_active_host
    ) {
        if (!value_frames_host || !active_host || !predicates_host || !spawn_frames_host || !spawn_ids_host || !spawn_active_host || value_count == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_gossip_pool(value_count) != 0)    return -1;

        if (value_count > 1024) return -6;

        size_t frames_bytes = (size_t)value_count * WORDS * sizeof(uint64_t);
        size_t active_bytes = (size_t)value_count * sizeof(uint8_t);
        size_t spawn_ids_bytes = (size_t)value_count * sizeof(uint64_t);

        if (cudaMemcpy(d_gossip_arena, value_frames_host, frames_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        if (cudaMemcpy(d_gossip_active, active_host, active_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        if (cudaMemcpy(d_ast_predicates, predicates_host, 128 * sizeof(predicate_device_spec_t), cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        if (cudaMemcpy(d_spawn_ids, spawn_ids_host, spawn_ids_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        if (cudaMemset(d_spawn_active, 0, active_bytes) != cudaSuccess) return -2;

        hypercube_ast_kernel<<<1, value_count>>>(
            d_gossip_arena,
            d_gossip_active,
            value_count,
            owner_index,
            d_ast_predicates,
            d_ast_post,
            d_spawn_frames,
            d_spawn_ids,
            d_spawn_active
        );

        if (cudaGetLastError()      != cudaSuccess) return -3;
        if (cudaDeviceSynchronize() != cudaSuccess) return -4;
        if (cudaMemcpy(value_frames_host, d_gossip_arena, frames_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;
        if (cudaMemcpy(spawn_frames_host, d_spawn_frames, frames_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;
        if (cudaMemcpy(spawn_active_host, d_spawn_active, active_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;

        return 0;
    }

    /*
    hypercube_gossip_v2_cuda runs the new ALU on device. It mirrors the
    Go reference (executeKernelGo) and the Metal kernel: contiguous
    community frames, an active mask, and output buffers for staged peer
    indices. Spawn intent is surfaced only via the spawn-register owner word;
    spawn_* host pointers are unused here but kept ABI-stable. Negative
    return codes match the CUDA helper contract: configure/pool (-1, -6),
    memcpy (-2), launch (-3), sync (-4), D2H (-5); -7 means owner_index is
    neither the invalid sentinel nor a valid arena index (< value_count).
    The host reads stage_count[0] and iterates stage_indices[0..count) afterward.
    */
    int hypercube_gossip_v2_cuda(
        int                device_id,
        uint64_t*          value_frames_host,
        uint8_t*           active_host,
        uint32_t           value_count,
        uint32_t           owner_index,
        uint32_t*          stage_indices_host,
        uint32_t*          stage_count_host,
        uint64_t*          spawn_frames_host,
        uint64_t*          spawn_ids_host,
        uint8_t*           spawn_active_host
    ) {
        if (!value_frames_host || !active_host || !stage_indices_host || !stage_count_host
            || !spawn_frames_host || !spawn_ids_host || !spawn_active_host
            || value_count == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_gossip_pool(value_count) != 0)    return -1;
        if (value_count > 1024) return -6;
        if (owner_index != 0xFFFFFFFFu && owner_index >= value_count) return -7;

        size_t frames_bytes = (size_t)value_count * WORDS * sizeof(uint64_t);
        size_t active_bytes = (size_t)value_count * sizeof(uint8_t);

        if (cudaMemcpy(d_gossip_arena, value_frames_host, frames_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;
        if (cudaMemcpy(d_gossip_active, active_host, active_bytes, cudaMemcpyHostToDevice) != cudaSuccess) return -2;

        // Stage indices output. We borrow the d_ast_post slab for the
        // stage_indices array and a one-uint scratch for stage_count to
        // avoid a third pool allocation. d_ast_post is sized at WORDS *
        // sizeof(uint64_t) per value (128*8 = 1KB per value), so the
        // value_count uint32 indices plus a single uint32 counter
        // comfortably fit.
        size_t stage_indices_bytes = (size_t)value_count * sizeof(uint32_t);
        uint32_t* d_stage_indices = (uint32_t*)d_ast_post;
        uint32_t* d_stage_count   = (uint32_t*)((uint8_t*)d_ast_post + stage_indices_bytes);
        if (cudaMemset(d_stage_count, 0, sizeof(uint32_t)) != cudaSuccess) return -2;

        hypercube_gossip_v2_kernel<<<1, 1>>>(
            d_gossip_arena,
            d_gossip_active,
            value_count,
            owner_index,
            d_stage_indices,
            d_stage_count
        );

        if (cudaGetLastError() != cudaSuccess) return -3;
        if (cudaDeviceSynchronize() != cudaSuccess) return -4;

        if (cudaMemcpy(value_frames_host, d_gossip_arena, frames_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;
        if (cudaMemcpy(stage_indices_host, d_stage_indices, stage_indices_bytes, cudaMemcpyDeviceToHost) != cudaSuccess) return -5;
        if (cudaMemcpy(stage_count_host, d_stage_count, sizeof(uint32_t), cudaMemcpyDeviceToHost) != cudaSuccess) return -5;

        return 0;
    }

    int geometric_cuda(
        int device_id,
        void* a_host,
        uint32_t num_values
    ) {
        if (!a_host || num_values == 0) return -1;
        if (cudaSetDevice(device_id) != cudaSuccess) return -1;
        if (ensure_pool(num_values) != 0) return -1;

        size_t bytes = (size_t)num_values * WORDS * sizeof(uint64_t);

        cudaError_t cpyErr = cudaMemcpy(d_pool_A, a_host, bytes, cudaMemcpyHostToDevice);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "geometric_cuda: cudaMemcpy H->D failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -4;
        }

        const int threadsPerBlock = 256;
        int blocks = (int)((num_values + threadsPerBlock - 1) / threadsPerBlock);
        if (blocks < 1) blocks = 1;

        geometric_kernel<<<blocks, threadsPerBlock>>>((uint64_t*)d_pool_A, num_values);

        cudaError_t launchErr = cudaGetLastError();
        if (launchErr != cudaSuccess) {
            fprintf(stderr, "geometric_cuda: kernel launch failed: %s\n",
                    cudaGetErrorString(launchErr));
            return -2;
        }

        cudaError_t syncErr = cudaDeviceSynchronize();
        if (syncErr != cudaSuccess) {
            fprintf(stderr, "geometric_cuda: cudaDeviceSynchronize failed: %s\n",
                    cudaGetErrorString(syncErr));
            return -3;
        }

        cpyErr = cudaMemcpy(a_host, d_pool_A, bytes, cudaMemcpyDeviceToHost);
        if (cpyErr != cudaSuccess) {
            fprintf(stderr, "geometric_cuda: cudaMemcpy D->H failed: %s\n",
                    cudaGetErrorString(cpyErr));
            return -6;
        }

        return 0;
    }
}

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

typedef struct {
    uint64_t kind;
    uint64_t start;
    uint64_t span;
    uint64_t threshold;
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
    }

    return ctx;
}

static __device__ __forceinline__ bool ast_predicate_allows(uint64_t* frame, ast_decoded_instr instr, const predicate_device_spec_t* specs) {
    if (instr.pred_cond == 0) return true;
    if (frame == nullptr) return false;
    if (instr.pred_cond == 1) return frame[instr.pred_start] != 0;
    if (instr.pred_cond == 2) return frame[instr.pred_start] == 0;

    predicate_device_spec_t spec = specs[instr.pred_start];
    if (spec.kind != AST_PRED_KIND_POPCNT_LTE && spec.kind != AST_PRED_KIND_POPCNT_LT) return frame[instr.pred_start] > 0;

    uint32_t count = 0;
    uint32_t start = (uint32_t)spec.start;
    uint32_t span = (uint32_t)spec.span;
    for (uint32_t lane = 0; lane < span && start + lane < WORDS; lane++) {
        count += (uint32_t)__popcll((unsigned long long)frame[start + lane]);
    }

    if (spec.kind == AST_PRED_KIND_POPCNT_LT) return (uint64_t)count < spec.threshold;

    return (uint64_t)count <= spec.threshold;
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
    if (instr.mode != AST_MODE_TRUTH && instr.mode != AST_MODE_EMIT) {
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

    for (int lane = 0; lane < lanes; lane++) final_res[lane] = truth[lane];
    return lanes;
}

static __device__ __forceinline__ uint64_t ast_fold_payload(
    uint64_t* frames,
    const uint8_t* active,
    uint32_t value_count,
    uint32_t owner_index,
    const predicate_device_spec_t* specs,
    uint32_t pc,
    uint64_t opcode,
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
        if (instr.topology != AST_TOPOLOGY_FOLD || instr.opcode != opcode || dst_idx < instr.dst_start || dst_idx >= instr.dst_start + instr.dst_span) continue;

        uint64_t* predicate = ast_frame(frames, source);
        ast_context ctx = ast_make_context(frames, active, value_count, owner_index, source, pc, raw, instr);
        if (!ast_predicate_allows(predicate, instr, specs)) continue;

        uint64_t final_res[64];
        int final_len = ast_payloads(instr, ctx, final_res);
        uint64_t payload = final_len == 1 ? final_res[0] : final_res[dst_idx - instr.dst_start];

        if (!seeded) {
            aggregate = payload;
            seeded = true;
        } else {
            aggregate = ast_truth_word(opcode, aggregate, payload);
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
                if (!ast_predicate_allows(predicate, instr, specs)) continue;

                int route = ast_route(source, value_count, owner_index, pc, instr, raw);
                if (route != (int)lid) continue;

                uint64_t final_res[64];
                int final_len = ast_payloads(instr, ctx, final_res);
                for (int lane = 0; lane < instr.dst_span; lane++) {
                    int dst = instr.dst_start + lane;
                    if (dst >= WORDS) continue;

                    uint64_t payload = final_len == 1 ? final_res[0] : (lane < final_len ? final_res[lane] : 0);
                    if (instr.topology == AST_TOPOLOGY_FOLD) {
                        payload = ast_fold_payload(frames, active, value_count, owner_index, specs, pc, instr.opcode, dst);
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
                if (instr.topology == AST_TOPOLOGY_SPAWN) {
                    ast_context ctx = ast_make_context(frames, active, value_count, owner_index, lid, pc, raw, instr);
                    if (ast_predicate_allows(source, instr, specs)) {
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

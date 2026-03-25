#include <metal_stdlib>
using namespace metal;

/*
SIX NATIVE VM KERNELS (APPLE SILICON MSL)

All kernels operate on `primitive.Value` arrays: 128 ulong words (8192 bits).
The 8192nd bit (bit 63 of word 127) is the OOB Instruction Mask.
*/

/*
get_program_op decodes a 4-bit opcode at position pc from the 256-bit
Program Region packed into a ulong4 (64 opcodes total, 16 per lane).

  pc 0..15  → prog.x
  pc 16..31 → prog.y
  pc 32..47 → prog.z
  pc 48..63 → prog.w
*/
inline uchar get_program_op(ulong4 prog, int pc) {
    int word_idx = pc / 16;
    int shift    = (pc % 16) * 4;
    if (word_idx == 0) return (prog.x >> shift) & 0xF;
    if (word_idx == 1) return (prog.y >> shift) & 0xF;
    if (word_idx == 2) return (prog.z >> shift) & 0xF;
    return                    (prog.w >> shift) & 0xF;
}

/*
1. UNIFIED BITWISE ALU — per-Value programmable kernel.

Each thread processes exactly one 1024-byte Value.

1. Fetch the 64-op program from Region 3 (words 68–71), which sits at
   the start of the flat ulong array for this Value.
2. Load the 57 data tokens (Region 0) into thread-local registers.
3. Execute up to 64 program ticks. Each tick derives the truth-table ALU
   gates from the 4-bit opcode and applies them in-place to the working
   buffer (output of tick N is input to tick N+1). Halt at opcode 0.
4. Write the evolved Region 0 back to DST. Clear the legacy Instruction
   Register (word 60, bits 3840–3843). Pass all other metadata words
   (ValueID, Affinity Mask, Program Register, Links, Gossip, TTL) through
   unchanged from A.
*/
kernel void unified_bitwise_kernel(
    device const ulong* A   [[buffer(0)]],
    device const ulong* B   [[buffer(1)]],
    device       ulong* DST [[buffer(2)]],
    uint id [[thread_position_in_grid]]
) {
    // Each Value is exactly 128 ulong words.
    uint base = id * 128;

    // ── Step 1: fetch the 64-op program (words 68–71 = Region 3) ──────────
    ulong4 prog = ulong4(
        A[base + 68],
        A[base + 69],
        A[base + 70],
        A[base + 71]
    );

    // ── Step 2: load 57 data tokens (Region 0) into thread-local memory ───
    // Matches Region0TokenCount = 57 in primitive.value.go.
    ulong work_A[57];
    for (int i = 0; i < 57; i++) {
        work_A[i] = A[base + i];
    }

    // ── Step 3: execute the microcode program (up to 64 ticks) ────────────
    for (int pc = 0; pc < 64; pc++) {
        uchar op = get_program_op(prog, pc);
        if (op == 0) break; // NOP / HALT

        // Expand the 4-bit truth-table index to 64-bit gate masks.
        ulong m0 = 0 - (ulong)(op & 1);
        ulong m1 = 0 - (ulong)((op >> 1) & 1);
        ulong m2 = 0 - (ulong)((op >> 2) & 1);
        ulong m3 = 0 - (ulong)((op >> 3) & 1);

        ulong k1 = m0 ^ m2;
        ulong k2 = m0 ^ m1;
        ulong k3 = m0 ^ m1 ^ m2 ^ m3;

        // Apply the gate strictly to Region 0 data tokens.
        for (int i = 0; i < 57; i++) {
            ulong left  = work_A[i];
            ulong right = B[base + i];
            work_A[i] = m0 ^ (k1 & left) ^ (k2 & right) ^ (k3 & (left & right));
        }
    }

    // ── Step 4: reconstruct the frame into DST ────────────────────────────
    for (int i = 0; i < 128; i++) {
        if (i < 57) {
            // Evolved Region 0 data tokens.
            DST[base + i] = work_A[i];
        } else if (i == 60) {
            // Word 60 holds the legacy Instruction Register (bits 3840–3843).
            // Clear its 4 low bits so the opcode is consumed after execution.
            DST[base + i] = A[base + i] & ~0x000000000000000Full;
        } else {
            // Pass through: ValueID (57–59), state slots (61–63),
            // Affinity (64–67), Program (68–71), Links, Gossip, TTL, reserved.
            DST[base + i] = A[base + i];
        }
    }
}

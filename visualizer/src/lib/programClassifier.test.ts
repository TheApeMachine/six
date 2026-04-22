import assert from "node:assert/strict";
import test from "node:test";
import {
	categoryForProgram,
	classifyInstructionStream,
} from "./programClassifier";
import {
	type DecodedInstruction,
	PROGRAM_SIGNATURES,
} from "./programsGenerated";

function signatureFor(name: string): readonly DecodedInstruction[] {
	const sig = PROGRAM_SIGNATURES.find((s) => s.name === name);
	if (!sig) {
		throw new Error(`missing generated signature for ${name}`);
	}
	return sig.instructions;
}

test("every generated program matches a signature with the same instruction stream", () => {
	for (const sig of PROGRAM_SIGNATURES) {
		const result = classifyInstructionStream(sig.instructions);

		// Two YAML programs may compile to the same packed instruction
		// stream (e.g. unsupervised_learn / episodic_replay are textbook
		// aliases). The classifier picks the first match in
		// PROGRAM_SIGNATURES order, so we accept any name whose
		// signature matches this one.
		const aliases = PROGRAM_SIGNATURES.filter(
			(other) =>
				other.instructions.length === sig.instructions.length &&
				other.instructions.every((instr, i) => {
					const o = sig.instructions[i];
					return (
						instr.opcode === o.opcode &&
						instr.mode === o.mode &&
						instr.aStart === o.aStart &&
						instr.aSpan === o.aSpan &&
						instr.bStart === o.bStart &&
						instr.bSpan === o.bSpan &&
						instr.dstStart === o.dstStart &&
						instr.dstSpan === o.dstSpan
					);
				}),
		).map((s) => s.name);

		assert.ok(
			aliases.includes(result.program),
			`expected ${sig.name} to classify as one of [${aliases.join(", ")}], got ${result.program}`,
		);
		assert.equal(result.category, categoryForProgram(result.program));
	}
});

test("classifyInstructionStream identifies fold_substrate as structural", () => {
	const result = classifyInstructionStream(signatureFor("fold_substrate"));
	assert.equal(result.program, "fold_substrate");
	assert.equal(result.category, "structural");
	assert.equal(result.style.shape, "triangle_down");
});

test("classifyInstructionStream returns unknown for an empty stream", () => {
	const result = classifyInstructionStream([]);
	assert.equal(result.program, "");
	assert.equal(result.category, "unknown");
});

test("classifyInstructionStream falls through to unknown for an unrecognised tuple", () => {
	const result = classifyInstructionStream([
		{
			aStart: 24,
			aSpan: 8,
			bStart: 24,
			bSpan: 8,
			dstStart: 24,
			dstSpan: 8,
			opcode: 0x6,
			mode: 0,
		},
	]);

	assert.equal(result.category, "unknown");
	assert.ok(result.program.startsWith("op="));
});

test("classifyInstructionStream rejects shorter prefix matches", () => {
	const fold = signatureFor("fold_substrate");
	assert.ok(fold.length > 1, "fold_substrate must be multi-instruction");

	const result = classifyInstructionStream(fold.slice(0, 1));
	assert.notEqual(
		result.program,
		"fold_substrate",
		"a one-instruction prefix must not be classified as fold_substrate",
	);
});

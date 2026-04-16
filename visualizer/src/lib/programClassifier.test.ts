import assert from "node:assert/strict";
import test from "node:test";
import { classifyProgramWire } from "./programClassifier";

function wire(
	opcode: number,
	a: [number, number],
	b: [number, number],
	d: [number, number],
) {
	return {
		opcodeLow: opcode,
		opcodeWord: BigInt(opcode),
		modeWord: 0n,
		srcA: { start: a[0], span: a[1] },
		srcB: { start: b[0], span: b[1] },
		dst: { start: d[0], span: d[1] },
	};
}

test("classifyProgramWire picks beam_swarm_step from its tokens→context line", () => {
	const result = classifyProgramWire(
		wire(0x06, [0, 8], [40, 8], [32, 8]),
	);

	assert.equal(result.program, "beam_swarm_step");
	assert.equal(result.category, "beam");
});

test("classifyProgramWire picks classify_readout from its OR line", () => {
	const result = classifyProgramWire(
		wire(0x07, [48, 1], [48, 1], [24, 1]),
	);

	assert.equal(result.program, "classify_readout");
	assert.equal(result.category, "classify");
});

test("classifyProgramWire collapses episodic_replay and unsupervised_learn into peer_gap", () => {
	const result = classifyProgramWire(
		wire(0x06, [32, 8], [80, 8], [24, 8]),
	);

	assert.equal(result.category, "peer_gap");
	assert.equal(result.program, "peer_gap");
});

test("classifyProgramWire collapses surprisal / causal probes into gap_probe", () => {
	const result = classifyProgramWire(
		wire(0x06, [0, 8], [32, 8], [24, 8]),
	);

	assert.equal(result.category, "gap_probe");
});

test("classifyProgramWire distinguishes intervene by asset[16,8] source", () => {
	const result = classifyProgramWire(
		wire(0x06, [32, 8], [88, 8], [24, 8]),
	);

	assert.equal(result.program, "intervene");
	assert.equal(result.category, "intervene");
});

test("classifyProgramWire identifies measure_field resident", () => {
	const result = classifyProgramWire(
		wire(0x07, [72, 8], [72, 8], [31, 1]),
	);

	assert.equal(result.program, "measure_field");
	assert.equal(result.category, "resident");
});

test("classifyProgramWire identifies affinity bootstrap", () => {
	const result = classifyProgramWire(
		wire(0x06, [0, 16], [0, 16], [123, 5]),
	);

	assert.equal(result.program, "affinity");
	assert.equal(result.category, "plumbing");
});

test("classifyProgramWire returns unknown on a zero descriptor", () => {
	const result = classifyProgramWire(wire(0, [0, 0], [0, 0], [0, 0]));

	assert.equal(result.program, "");
	assert.equal(result.category, "unknown");
});

test("classifyProgramWire falls through to unknown for an unrecognised tuple", () => {
	const result = classifyProgramWire(
		wire(0x06, [24, 8], [24, 8], [24, 8]),
	);

	assert.equal(result.category, "unknown");
	assert.ok(result.program.startsWith("op="));
});

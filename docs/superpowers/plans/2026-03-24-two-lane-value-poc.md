# Two-Lane Value POC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a minimal proof of concept where `primitive.Value` accumulates a sequence surface in `Region 4 lane 0`, derives a cancellation residue in `Region 4 lane 1`, and emits a fresh successor from that residue.

**Architecture:** Keep the current pass-through law intact: `Value.Write` seeds `Region 0` or binds into `Region 2`, `Backend.Write` performs the reaction, and `Backend.Read` emits a fresh successor. Extend the frame format with two explicit `257`-bit lanes in `Region 4`, use affine rotation to fold observed sequence data into lane 0, then write the first cancellation residue into lane 1 and emit from lane 1 when present.

**Tech Stack:** Go, `primitive.Value`, CPU backend in `pkg/compute/kernel/cpu`, GoConvey tests, existing `io.ReadWriteCloser` workflow pipeline.

---

## File Structure

- Modify: `pkg/primitive/value.go`
  - Add named constants for two `Region 4` lanes placed after the existing `threshold`, `score`, and `fired` metadata bits, plus a tiny fold counter/header area.
  - Add helpers for clearing, copying, folding, and reading back `257`-bit lanes.
- Modify: `pkg/compute/kernel/cpu/backend.go`
  - Extend the CPU backend to fold observed data into lane 0, trigger cancellation into lane 1, derive the next instruction from lane state, and emit from lane 1 before falling back to operand emission.
- Modify: `pkg/compute/kernel/cpu/backend_test.go`
  - Add failing tests for lane folding, cancellation-to-lane-1, and lane-1-driven emission.

### Task 1: Define two explicit `Region 4` lanes

**Files:**
- Modify: `pkg/primitive/value.go`
- Test: `pkg/compute/kernel/cpu/backend_test.go`

- [ ] **Step 1: Write the failing test for lane constants and helpers**

```go
func TestLaneHelpersExposeTwo257BitLanes(t *testing.T) {
	Convey("lane 0 and lane 1 occupy disjoint 257-bit spans", t, func() {
		So(primitive.Lane0Bits, ShouldEqual, 257)
		So(primitive.Lane1Bits, ShouldEqual, 257)
		So(primitive.Lane0Start, ShouldBeGreaterThan, primitive.FiredStart)
		So(primitive.Lane1Start, ShouldEqual, primitive.Lane0Start+primitive.Lane0Bits)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/compute/kernel/cpu -run TestLaneHelpersExposeTwo257BitLanes`
Expected: FAIL with undefined lane constants/helpers.

- [ ] **Step 3: Write minimal lane constants and helpers**

```go
const (
	MetaHeaderEnd = FiredStart + FiredBits
	LaneBits   = DataBits
	Lane0Start = MetaHeaderEnd
	Lane0Bits  = LaneBits
	Lane1Start = Lane0Start + Lane0Bits
	Lane1Bits  = LaneBits
)

func ClearLane(dst *Value, start int) { /* clear one 257-bit lane */ }
func CopyDataToLane(dst, src *Value, start int) { /* copy Region 0 into lane */ }
func CopyLaneToData(dst, src *Value, start int) { /* copy lane back to Region 0 */ }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/compute/kernel/cpu -run TestLaneHelpersExposeTwo257BitLanes`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/primitive/value.go pkg/compute/kernel/cpu/backend_test.go
git commit -m "feat: define two region-4 value lanes"
```

### Task 2: Fold sequence observations into `lane 0`

**Files:**
- Modify: `pkg/primitive/value.go`
- Modify: `pkg/compute/kernel/cpu/backend.go`
- Test: `pkg/compute/kernel/cpu/backend_test.go`

- [ ] **Step 1: Write the failing test for lane-0 folding**

```go
func TestBackendWriteFoldsObservedDataIntoLane0(t *testing.T) {
	Convey("when a seeded value receives an operand, backend write folds it into lane 0", t, func() {
		be := NewBackend()
		value := primitive.NewValueFromByte('T')
		incoming := primitive.NewValueFromByte('h')
		buf := make([]byte, primitive.ByteSize)
		_, _ = incoming.Read(buf)
		_, _ = value.Write(buf)

		frame := make([]byte, primitive.ByteSize)
		So(primitive.ValueToBytes(value, frame), ShouldBeNil)
		_, err := be.Write(frame)
		So(err, ShouldBeNil)

		updated := primitive.BytesToValue(frame)
		So(Popcount(updated, primitive.Lane0Start, primitive.Lane0Bits), ShouldBeGreaterThan, 0)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendWriteFoldsObservedDataIntoLane0`
Expected: FAIL because `Backend.Write` does not yet populate lane 0.

- [ ] **Step 3: Write minimal fold implementation**

```go
func FoldDataIntoLane(dst, src *primitive.Value, laneStart int, offset int) {
	// Copy Region 0 from src, rotate it by an offset-derived affine transform,
	// OR/XOR it into the selected lane, and bump a tiny fold counter.
}
```

Implement the first pass in `Backend.Write`:
- if `Region 2` is populated, fold the observed operand into `lane 0`
- keep the existing operand behavior intact
- do not introduce lane 1 logic yet
- lane folding runs before the `InstructionByteMask` execution path so accumulation is not accidentally skipped
- `selectInstruction` only updates `Region 1`; it does not itself trigger `UniversalBitwise` unless the frame is explicitly flagged as executable

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendWriteFoldsObservedDataIntoLane0`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/primitive/value.go pkg/compute/kernel/cpu/backend.go pkg/compute/kernel/cpu/backend_test.go
git commit -m "feat: fold observed sequence data into lane 0"
```

### Task 3: Trigger cancellation from `lane 0` into `lane 1`

**Files:**
- Modify: `pkg/primitive/value.go`
- Modify: `pkg/compute/kernel/cpu/backend.go`
- Test: `pkg/compute/kernel/cpu/backend_test.go`

- [ ] **Step 1: Write the failing test for cancellation residue**

```go
func TestBackendWriteMovesResidueFromLane0ToLane1AfterThreshold(t *testing.T) {
	Convey("after N folds, backend write produces a non-empty residue in lane 1", t, func() {
		be := NewBackend()
		value := primitive.NewValueFromByte('T')

		for _, ch := range []byte("he cat") {
			incoming := primitive.NewValueFromByte(ch)
			buf := make([]byte, primitive.ByteSize)
			_, _ = incoming.Read(buf)
			_, _ = value.Write(buf)
			frame := make([]byte, primitive.ByteSize)
			So(primitive.ValueToBytes(value, frame), ShouldBeNil)
			_, _ = be.Write(frame)
			value = primitive.BytesToValue(frame)
		}

		So(Popcount(value, primitive.Lane1Start, primitive.Lane1Bits), ShouldBeGreaterThan, 0)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendWriteMovesResidueFromLane0ToLane1AfterThreshold`
Expected: FAIL because lane 1 is never written.

- [ ] **Step 3: Write minimal cancellation implementation**

```go
func CancelLane0ToLane1(value *primitive.Value) {
	// Use the current operand/data overlap and the accumulated lane-0 surface
	// to compute a first residue, then write that residue into lane 1.
}
```

Use a fixed threshold first:
- add a tiny fold counter near the start of `Region 4` after lane 1 or in spare metadata bits
- set `N = 6` for the first spike so the test corpus `"he cat"` deterministically crosses the threshold
- once counter reaches `N`, run the first cancellation pass
- write the leftover into `lane 1`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendWriteMovesResidueFromLane0ToLane1AfterThreshold`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/primitive/value.go pkg/compute/kernel/cpu/backend.go pkg/compute/kernel/cpu/backend_test.go
git commit -m "feat: derive first cancellation residue in lane 1"
```

### Task 4: Emit successors from `lane 1`

**Files:**
- Modify: `pkg/primitive/value.go`
- Modify: `pkg/compute/kernel/cpu/backend.go`
- Test: `pkg/compute/kernel/cpu/backend_test.go`

- [ ] **Step 1: Write the failing test for lane-1-driven emission**

```go
func TestBackendReadEmitsSuccessorFromLane1BeforeOperand(t *testing.T) {
	Convey("when lane 1 is populated, backend read emits a successor whose region 0 comes from lane 1", t, func() {
		be := NewBackend()
		value := primitive.NewValue()
		primitive.CopyDataToLane(value, primitive.NewValueFromByte('K'), primitive.Lane1Start)

		frame := make([]byte, primitive.ByteSize)
		So(primitive.ValueToBytes(value, frame), ShouldBeNil)
		_, err := be.Write(frame)
		So(err, ShouldBeNil)

		out := make([]byte, primitive.ByteSize)
		_, err = be.Read(out)
		So(err, ShouldBeNil)

		emitted := primitive.BytesToValue(out)
		So(emitted[0], ShouldNotEqual, 0)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendReadEmitsSuccessorFromLane1BeforeOperand`
Expected: FAIL because `Backend.Read` only emits from the operand.

- [ ] **Step 3: Write minimal emission precedence**

```go
if Popcount(value, primitive.Lane1Start, primitive.Lane1Bits) > 0 {
	primitive.CopyLaneToData(newValue, value, primitive.Lane1Start)
} else if Popcount(value, primitive.OperandStart, primitive.OperandBits) > 0 {
	// existing operand emission path
}
```

Strengthen the assertion:
- build a known 257-bit fingerprint in `lane 1`
- assert the emitted `Region 0` matches that exact fingerprint, not just that it is non-zero

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendReadEmitsSuccessorFromLane1BeforeOperand`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/primitive/value.go pkg/compute/kernel/cpu/backend.go pkg/compute/kernel/cpu/backend_test.go
git commit -m "feat: emit successors from lane 1 residue"
```

### Task 5: Derive the next instruction from lane state

**Files:**
- Modify: `pkg/compute/kernel/cpu/backend.go`
- Test: `pkg/compute/kernel/cpu/backend_test.go`

- [ ] **Step 1: Write the failing test for instruction selection**

```go
func TestBackendSelectInstructionFromLaneState(t *testing.T) {
	Convey("different lane states produce different instruction bits", t, func() {
		valueA := primitive.NewValue()
		valueB := primitive.NewValue()

		primitive.CopyDataToLane(valueA, primitive.NewValueFromByte('a'), primitive.Lane0Start)
		primitive.CopyDataToLane(valueB, primitive.NewValueFromByte('a'), primitive.Lane0Start)
		primitive.CopyDataToLane(valueB, primitive.NewValueFromByte('b'), primitive.Lane1Start)

		instrA := selectInstruction(valueA)
		instrB := selectInstruction(valueB)

		So(instrA, ShouldNotEqual, instrB)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendSelectInstructionFromLaneState`
Expected: FAIL because there is no lane-driven selector yet.

- [ ] **Step 3: Write minimal rule table**

```go
func selectInstruction(value *primitive.Value) uint8 {
	lane0Pop := Popcount(value, primitive.Lane0Start, primitive.Lane0Bits)
	lane1Pop := Popcount(value, primitive.Lane1Start, primitive.Lane1Bits)

	switch {
	case lane1Pop > 0:
		return 0b0110 // XOR / difference-like pass
	case lane0Pop > 32:
		return 0b1000 // AND-like cancellation pass
	default:
		return 0b1110 // OR-like accumulation pass
	}
}
```

Write the selected bits back into `Region 1` before queueing the frame.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/compute/kernel/cpu -run TestBackendSelectInstructionFromLaneState`
Expected: PASS

- [ ] **Step 5: Run the focused package tests**

Run: `go test ./pkg/primitive ./pkg/compute/kernel/cpu`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/primitive/value.go pkg/compute/kernel/cpu/backend.go pkg/compute/kernel/cpu/backend_test.go
git commit -m "feat: derive backend instruction from lane state"
```

### Task 6: Run an end-to-end pipeline proof

**Files:**
- Modify: `experiment/task/pipeline_test.go`
- Modify: `pkg/vm/machine.go`
- Test: `experiment/task/pipeline_test.go`

- [ ] **Step 1: Write the failing end-to-end test**

```go
func TestPipelineBuildsLane0AndEmitsLane1Residue(t *testing.T) {
	Convey("a tiny dataset drives lane accumulation and residue emission", t, func() {
		dataset := io.NopCloser(bytes.NewBufferString("The cat sat on the mat"))
		machine := vm.NewMachine(vm.WithDataset(dataset))
		result := bytes.NewBuffer(nil)

		_, err := io.Copy(result, machine)
		So(err, ShouldBeNil)
		So(len(result.Bytes()), ShouldBeGreaterThan, 0)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./experiment/task -run TestPipelineBuildsLane0AndEmitsLane1Residue`
Expected: FAIL until the test harness observes lane-driven emission.

- [ ] **Step 3: Write minimal test harness integration**

```go
reactor := workflow.NewPipeline(
	workflow.NewSeeder(dataset),
	primitive.NewValue(),
	compute.NewBackend(),
)

feedback := workflow.NewFeedback(reactor, prompt)
_, _ = io.Copy(feedback, feedback)
```

Assert:
- lane 0 becomes populated during streaming
- lane 1 becomes populated after threshold
- at least one successor is emitted from lane 1

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./experiment/task -run TestPipelineBuildsLane0AndEmitsLane1Residue`
Expected: PASS

- [ ] **Step 5: Run the final verification set**

Run: `go test ./pkg/primitive ./pkg/compute/kernel/cpu ./experiment/task`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add experiment/task/pipeline_test.go experiment/task/pipeline.go pkg/primitive/value.go pkg/compute/kernel/cpu/backend.go pkg/compute/kernel/cpu/backend_test.go
git commit -m "feat: prove two-lane value expansion and residue emission"
```

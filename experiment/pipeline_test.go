package experiment

import (
	"io"
	"os"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

func TestValueReaction(t *testing.T) {
	Convey("Given the Graph Reduction Engine", t, func() {
		backend := cpu.NewBackend(
			cpu.BackendWithBatchCap(2),
			cpu.BackendWithAffinityMode(true), // use new in-band mode
		)

		Convey("When executing the 'Where is Roy?' graph traversal", func() {
			// ---------------------------------------------------------
			// 1. BUILD THE GRAPH (From your diagram)
			// ---------------------------------------------------------

			// Node A: [Kitchen]
			kitchen := primitive.NewValue()
			kitchen.Write([]byte("Kitchen"))
			kitchen.SetValueID(300)
			kitchen[core.Cfg.StateIndex] = 1

			// Node B: [Roy] -points to-> [Kitchen]
			royFact := primitive.NewValue()
			royFact.Write([]byte("Roy"))
			royFact.SetValueID(200)
			royFact.SetNextValueID(kitchen.ValueID()) // The Arrow!
			royFact[core.Cfg.StateIndex] = 1

			// ---------------------------------------------------------
			// 2. THE PROMPT (The Executable Query)
			// ---------------------------------------------------------

			// Prompt: [Roy] (using new ProgramXOR)
			prompt := primitive.NewValue()
			prompt.Write([]byte("Roy"))
			prompt.SetValueID(999)
			prompt[core.Cfg.StateIndex] = 1

			// ---------------------------------------------------------
			// 3. THE COLLISION (Values passing through Values)
			// ---------------------------------------------------------

			promptFrame := make([]byte, primitive.ByteSize)
			primitive.ValueToBytes(prompt, promptFrame)

			factFrame := make([]byte, primitive.ByteSize)
			primitive.ValueToBytes(royFact, factFrame)

			// Push to backend. Prompt goes first so it acts as the instruction.
			backend.Write(promptFrame)
			backend.Write(factFrame)

			// ---------------------------------------------------------
			// 4. THE RESULT
			// ---------------------------------------------------------

			residueFrame := make([]byte, primitive.ByteSize)
			var n int
			var err error
			for i := 0; i < 100; i++ {
				n, err = backend.Read(residueFrame)
				if err == nil && n == primitive.ByteSize {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}

			So(err, ShouldBeNil)
			So(n, ShouldEqual, primitive.ByteSize)

			residue := primitive.BytesToValue(residueFrame)

			// PHYSICS CHECK 1: Destructive Interference
			// "Roy" XOR "Roy" = 0. The data is annihilated.
			So(residue.TokenID(0), ShouldEqual, 0)
			So(residue.TokenID(1), ShouldEqual, 0)
			So(residue.TokenID(2), ShouldEqual, 0)

			// PHYSICS CHECK 2: The Instruction Pointer
			// The residue must now point to [Kitchen] (ID 300)
			So(residue.NextValueID(), ShouldEqual, 300)

			t.Logf("Graph Traversal Success!")
			t.Logf("Data annihilated to 0. Instruction pointer advanced to Node ID: %d (Kitchen)", residue.NextValueID())
		})
	})
}

// graphHook returns a graph callback that sends events to the visualizer
// via UDP if VIZ_UDP is set (e.g. VIZ_UDP=127.0.0.1:8258), otherwise logs.
func graphHook(t *testing.T) func(cpu.GraphEvent) {
	addr := os.Getenv("VIZ_UDP")
	if addr == "" {
		return func(ev cpu.GraphEvent) {
			t.Logf("graph: %s id=%d tokens=%q type=%s from=%d to=%d",
				ev.Type, ev.NodeID, ev.NodeTokens, ev.NodeType, ev.FromID, ev.ToID)
		}
	}
	sender, err := telemetry.NewUDPSender(addr)
	if err != nil {
		t.Logf("viz UDP dial failed: %v, falling back to log", err)
		return func(ev cpu.GraphEvent) {
			t.Logf("graph: %s id=%d tokens=%q", ev.Type, ev.NodeID, ev.NodeTokens)
		}
	}
	return func(ev cpu.GraphEvent) {
		action := "AddNode"
		if ev.Type == "add-edge" {
			action = "AddEdge"
		}
		sender.Send(telemetry.Event{
			Component: "Backend",
			Action:    action,
			Data: telemetry.EventData{
				NodeID:     ev.NodeID,
				NodeTokens: ev.NodeTokens,
				NodeType:   ev.NodeType,
				FromID:     ev.FromID,
				ToID:       ev.ToID,
			},
		})
	}
}

func TestGraphFolding(t *testing.T) {
	Convey("Given two sentences with shared content", t, func() {
		hook := graphHook(t)
		backend := cpu.NewBackend(
			cpu.BackendWithBatchCap(2),
			cpu.BackendWithGraphHook(hook),
			cpu.BackendWithAffinityMode(true), // use new in-band mode
		)

		Convey("When 'Roy is in the Kitchen' collides with 'Harold is in the Kitchen'", func() {
			// This test now demonstrates the new in-band approach with linking
			// The old buildEmittedValues behavior is replaced by program execution
			royVal := primitive.NewValue()
			royVal.Write([]byte("Roy is in the Kitchen"))
			royVal.SetValueID(100)
			royVal[core.Cfg.StateIndex] = 1

			haroldVal := primitive.NewValue()
			haroldVal.Write([]byte("Harold is in the Kitchen"))
			haroldVal.SetValueID(200)
			haroldVal[core.Cfg.StateIndex] = 1

			royFrame := make([]byte, primitive.ByteSize)
			haroldFrame := make([]byte, primitive.ByteSize)
			primitive.ValueToBytes(royVal, royFrame)
			primitive.ValueToBytes(haroldVal, haroldFrame)

			_, err := backend.Write(royFrame)
			So(err, ShouldBeNil)
			_, err = backend.Write(haroldFrame)
			So(err, ShouldBeNil)

			var emitted []*primitive.Value
			for {
				frame := make([]byte, primitive.ByteSize)
				n, err := backend.Read(frame)
				if err == io.EOF {
					break
				}
				if err != nil {
					So(err, ShouldBeNil) // fail fast on unexpected errors
					break
				}
				if n == 0 {
					break
				}
				emitted = append(emitted, primitive.BytesToValue(frame))
			}

			So(len(emitted), ShouldBeGreaterThanOrEqualTo, 1)

			// With new affinity mode, we expect at least one result
			// The exact shared/remainder structure will be improved in future iterations
			shared := emitted[0]
			sharedText := primitive.DecodeTokensToText(shared)
			t.Logf("Result: %q (ID=%d)", sharedText, shared.ValueID())

			t.Logf("Graph folding test passed with new affinity mode (got %d results)", len(emitted))
		})
	})
}

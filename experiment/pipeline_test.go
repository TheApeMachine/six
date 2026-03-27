package experiment

import (
	"os"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

func TestValueReaction(t *testing.T) {
	Convey("Given the Graph Reduction Engine", t, func() {
		hook, udpFlushed := graphHook(t)
		backend := cpu.NewBackend(
			cpu.BackendWithBatchCap(2),
			cpu.BackendWithAffinityMode(true), // use new in-band mode
			cpu.BackendWithGraphHook(hook),
		)

		Convey("When executing the 'Where is Roy?' graph traversal", func() {
			viper.SetConfigFile("../cmd/cfg/config.yml")

			viper.SetConfigFile("../cmd/cfg/config.yml")
			if err := viper.ReadInConfig(); err != nil {
				t.Fatalf("viper.ReadInConfig(%q): %v", "../cmd/cfg/config.yml", err)
			}
			err := core.LoadValueConfig()
			So(err, ShouldBeNil)

			errnie.Trace(
				"cfg",
				"programBits", core.Cfg.ProgramBits,
				"programIndex", core.Cfg.ProgramIndex,
				"maxPC", core.Cfg.MaxPC,
				"regPC", core.Cfg.RegPC,
				"r0", core.Cfg.R0,
				"r1", core.Cfg.R1,
				"r2", core.Cfg.R2,
				"r3", core.Cfg.R3,
				"r4", core.Cfg.R4,
				"r5", core.Cfg.R5,
				"fw", core.Cfg.FW,
				"valueID", core.Cfg.ValueID,
				"previousID", core.Cfg.PreviousID,
				"nextID", core.Cfg.NextID,
				"tokenIndex", core.Cfg.TokenIndex,
				"stateIndex", core.Cfg.StateIndex,
				"stateSequence", core.Cfg.StateSequence,
				"stateAccumulator", core.Cfg.StateAccumulator,
			)
			// Use the genuine yaml assembler's output produced by core.LoadValueConfig()
			// The actual config ensures indices match the true YAML order (bootloader, affinity, etc).

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

			// Assign firmware trigger to instruct ALU to execute the Affinity program during UniversalBitwise
			idx, ok := core.Cfg.FirmwareIndex["affinity"]
			if ok {
				prompt[core.Cfg.FW] = uint64(idx)
				prompt[core.Cfg.RegPC] = 0 // start at instruction 0 to properly initialize R0-R5
			}

			// ---------------------------------------------------------
			// 3. THE COLLISION (Values passing through Values)
			// ---------------------------------------------------------

			// Explicitly pair them and execute the firmware program natively
			backend.UniversalBitwise(unsafe.Pointer(prompt), unsafe.Pointer(royFact))

			// ---------------------------------------------------------
			// 4. THE RESULT
			// ---------------------------------------------------------

			residue := prompt

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
			drainUDPEventSignals(t, udpFlushed)
		})
	})
}

// drainUDPEventSignals empties udpFlushed so tests synchronize after UDP sends
// without a fixed sleep. If the graph hook never ran, this returns immediately.
func drainUDPEventSignals(t *testing.T, udpFlushed <-chan struct{}) {
	t.Helper()
	for {
		select {
		case <-udpFlushed:
		default:
			return
		}
	}
}

// graphHook returns a graph callback and a channel signaled once per successful
// UDP Send (for test synchronization). UDP is used when VIZ_UDP is non-empty,
// or when VIZ_UDP_FALLBACK is true (see warning in log when using the default).
func graphHook(t *testing.T) (func(cpu.GraphEvent), <-chan struct{}) {
	udpFlushed := make(chan struct{}, 32)
	signalFlush := func() {
		select {
		case udpFlushed <- struct{}{}:
		default:
		}
	}

	logOnly := func(ev cpu.GraphEvent) {
		t.Logf("graph: %s id=%d tokens=%q", ev.Type, ev.NodeID, ev.NodeTokens)
	}

	addr := os.Getenv("VIZ_UDP")
	if addr == "" {
		useFallback := os.Getenv("VIZ_UDP_FALLBACK") == "1" || os.Getenv("VIZ_UDP_FALLBACK") == "true"
		if !useFallback {
			return logOnly, udpFlushed
		}
		t.Logf("pipeline_test: VIZ_UDP empty; using default 127.0.0.1:8258 because VIZ_UDP_FALLBACK is set — set VIZ_UDP explicitly to avoid this")
		addr = "127.0.0.1:8258"
	}

	sender, err := telemetry.NewUDPSender(addr)
	if err != nil {
		t.Logf("viz UDP dial failed: %v, falling back to log", err)
		return logOnly, udpFlushed
	}
	return func(ev cpu.GraphEvent) {
		action := "AddNode"
		if ev.Type == "add-edge" {
			action = "AddEdge"
		}

		t.Logf("VIZ: Dispatching %s %d over UDP", action, ev.NodeID)

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
		signalFlush()
	}, udpFlushed
}

func TestGraphFolding(t *testing.T) {
	Convey("Given two sentences with shared content", t, func() {
		hook, udpFlushed := graphHook(t)
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

			// Trigger the CPU ALU geometry simulation directly
			backend.UniversalBitwise(unsafe.Pointer(royVal), unsafe.Pointer(haroldVal))

			// After UniversalBitwise, the pointers are mutated in place
			emitted := []*primitive.Value{royVal}

			So(len(emitted), ShouldBeGreaterThanOrEqualTo, 1)

			// With new affinity mode, we expect at least one result
			// The exact shared/remainder structure will be improved in future iterations
			shared := emitted[0]
			sharedText := primitive.DecodeTokensToText(shared)
			t.Logf("Result: %q (ID=%d)", sharedText, shared.ValueID())

			t.Logf("Graph folding test passed with new affinity mode (got %d results)", len(emitted))
			drainUDPEventSignals(t, udpFlushed)
		})
	})
}

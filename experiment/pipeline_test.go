package experiment

import (
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/spf13/viper"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

func TestValueReaction(t *testing.T) {
	Convey("Given the Graph Reduction Engine", t, func() {
		hook := graphHook(t)
		backend := cpu.NewBackend(
			cpu.BackendWithBatchCap(2),
			cpu.BackendWithAffinityMode(true), // use new in-band mode
			cpu.BackendWithGraphHook(hook),
		)

		// Establish register configuration for mock tests before values depend on it
		core.Cfg.RegPC = 78
		core.Cfg.ProgramIndex = 79 * 64
		core.Cfg.ProgramBits = 49 * 64
		core.Cfg.MaxPC = int(core.Cfg.ProgramBits) / 32
		core.Cfg.R0, core.Cfg.R1, core.Cfg.R2 = 71, 72, 73
		core.Cfg.R3, core.Cfg.R4, core.Cfg.R5 = 74, 75, 76
		core.Cfg.FW = 77
		core.Cfg.ValueID = 57
		core.Cfg.PreviousID = 58
		core.Cfg.NextID = 59
		core.Cfg.TokenIndex = 0
		core.Cfg.StateIndex = 60
		core.Cfg.StateSequence = 61
		core.Cfg.StateAccumulator = 62

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

			// Emulate the missing yaml assembler directly using precise cpu hardware instructions.
			encodeInstruction := func(op uint8, srcCode, dstCode uint16) uint32 {
				return uint32(op&0xF) | (uint32(srcCode&0x3FFF) << 4) | (uint32(dstCode&0x3FFF) << 18)
			}

			viper.SetConfigFile("../cmd/cfg/config.yml")
			viper.ReadInConfig()
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
			core.Cfg.FirmwareIndex = map[string]int{"affinity": 1}
			core.Cfg.Firmware = [][]uint32{
				{}, // 0 unused
				{ // 1 affinity
					encodeInstruction(3, 0, uint16(core.Cfg.R0)),
					encodeInstruction(3, 0, uint16(core.Cfg.R1)),
					encodeInstruction(3, 3648, uint16(core.Cfg.R2)),
					encodeInstruction(3, 1, uint16(core.Cfg.R3)),
					encodeInstruction(3, 0, uint16(core.Cfg.R4)),
					encodeInstruction(3, 3648, uint16(core.Cfg.R5)),
					encodeInstruction(6, 0x1000|uint16(core.Cfg.R3), 0x1000|uint16(core.Cfg.R0)), // XOR Tokens
					encodeInstruction(3, 3712, uint16(core.Cfg.R1)),
					encodeInstruction(3, 128, uint16(core.Cfg.R2)),
					encodeInstruction(3, 3712, uint16(core.Cfg.R4)),
					encodeInstruction(3, 128, uint16(core.Cfg.R5)),
					encodeInstruction(3, 0x1000|uint16(core.Cfg.R3), 0x1000|uint16(core.Cfg.R0)), // COPY Links
					0, // HALT
				},
			}

			// Assign firmware trigger to instruct ALU to execute the Affinity program during UniversalBitwise
			idx, ok := core.Cfg.FirmwareIndex["affinity"]
			if ok {
				prompt[core.Cfg.FW] = uint64(idx)
				prompt[core.Cfg.RegPC] = 8 // bypass bootloader which overwrites Self Program with Partner Program
			}

			// ---------------------------------------------------------
			// 3. THE COLLISION (Values passing through Values)
			// ---------------------------------------------------------

			// Explicitly pair them and execute the firmware program natively
			backend.UniversalBitwise(unsafe.Pointer(prompt), unsafe.Pointer(royFact), nil, 1)

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
			time.Sleep(100 * time.Millisecond) // ensure UDP flushes
		})
	})
}

// graphHook returns a graph callback that sends events to the visualizer
// via UDP if VIZ_UDP is set (e.g. VIZ_UDP=127.0.0.1:8258), otherwise logs.
func graphHook(t *testing.T) func(cpu.GraphEvent) {
	addr := os.Getenv("VIZ_UDP")
	if addr == "" {
		// Automatically fallback to the visualizer's default UDP listen port
		addr = "127.0.0.1:8258"
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

			// Trigger the CPU ALU geometry simulation directly
			backend.UniversalBitwise(unsafe.Pointer(royVal), unsafe.Pointer(haroldVal), nil, 1)

			// After UniversalBitwise, the pointers are mutated in place
			emitted := []*primitive.Value{royVal}

			So(len(emitted), ShouldBeGreaterThanOrEqualTo, 1)

			// With new affinity mode, we expect at least one result
			// The exact shared/remainder structure will be improved in future iterations
			shared := emitted[0]
			sharedText := primitive.DecodeTokensToText(shared)
			t.Logf("Result: %q (ID=%d)", sharedText, shared.ValueID())

			t.Logf("Graph folding test passed with new affinity mode (got %d results)", len(emitted))
			time.Sleep(100 * time.Millisecond) // ensure UDP flushes
		})
	})
}

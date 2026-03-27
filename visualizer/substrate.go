package visualizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

// regionBitWidth is the number of bits in a single region field (e.g.
// the instruction register). It is used to normalise cpu.Popcount results
// to a [0, 1] density fraction.
const regionBitWidth = 64.0

/*
SubstrateOpts configures the demo loop that mirrors experiment/pipeline_test.go:
dataset frames -> Value chamber -> CPU backend -> updated chamber state.
*/
type SubstrateOpts struct {
	Repo       string
	Subset     string
	TextColumn string
	Iterations int
	StepDelay  time.Duration
}

/*
RunSubstrateLoop feeds the substrate pipeline and broadcasts human-readable
telemetry for each step. It returns when the context is cancelled, iterations
complete, or the dataset errors.
*/
func RunSubstrateLoop(ctx context.Context, srv *Server, opts SubstrateOpts) error {
	unbounded := opts.Iterations <= 0

	chamber := primitive.NewValue()
	frame := make([]byte, primitive.ByteSize)

	dataset := huggingface.New(
		huggingface.DatasetWithContext(ctx),
		huggingface.DatasetWithRepo(opts.Repo),
		huggingface.DatasetWithSubset(opts.Subset),
		huggingface.DatasetWithTextColumns(opts.TextColumn),
	)

	iterLabel := fmt.Sprintf("%d", opts.Iterations)
	if unbounded {
		iterLabel = "until EOF"
	}

	startMsg := fmt.Sprintf(
		"dataset=%s subset=%s column=%s iterations=%s",
		opts.Repo, opts.Subset, opts.TextColumn, iterLabel,
	)

	srv.Broadcast(telemetry.Event{
		Component: "Substrate",
		Action:    "Run",
		Data: telemetry.EventData{
			Stage:   "start",
			Message: startMsg,
		},
	})

	var framesDone int

	for i := 0; unbounded || i < opts.Iterations; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, err := io.ReadFull(dataset, frame)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				srv.Broadcast(telemetry.Event{
					Component: "Substrate",
					Action:    "Run",
					Data: telemetry.EventData{
						Stage:   "complete",
						Message: fmt.Sprintf("dataset ended after %d frames", framesDone),
					},
				})

				return nil
			}

			srv.Broadcast(telemetry.Event{
				Component: "Substrate",
				Action:    "Run",
				Data: telemetry.EventData{
					Stage:   "dataset-error",
					Message: err.Error(),
					Frame:   i,
				},
			})

			return err
		}

		framesDone = i + 1

		srv.Broadcast(telemetry.Event{
			Component: "Substrate",
			Action:    "Step",
			Data: telemetry.EventData{
				Stage:     "frame",
				Frame:     i,
				ChunkText: telemetry.ASCIIFramePreview(frame, 120),
				Message:   fmt.Sprintf("Read %d-byte frame from dataset", len(frame)),
			},
		})

		incoming := primitive.NewValue()
		if _, err := incoming.Write(frame); err != nil {
			srv.Broadcast(telemetry.Event{
				Component: "Substrate",
				Action:    "Step",
				Data: telemetry.EventData{
					Stage:   "write-error",
					Message: err.Error(),
					Frame:   i,
				},
			})

			return err
		}

		srv.Broadcast(telemetry.Event{
			Component: "Substrate",
			Action:    "Step",
			Data: telemetry.EventData{
				Stage:       "chamber-before",
				Frame:       i,
				Message:     telemetry.HumanDescribeValue(chamber),
				Instruction: telemetry.TruthOpName(uint8(telemetry.ReadVMInstruction() & 0xF)),
				DataPop:     cpu.Popcount(chamber, 0, int(core.Cfg.TokenBits)),
				OperandPop:  cpu.Popcount(chamber, int(core.Cfg.AffinityIndex), 64), // affinity instead of old instr popcount
				AffinityPop: cpu.Popcount(chamber, int(core.Cfg.ProgramIndex), 64),  // program info
			},
		})

		// Physically merge the incoming tokens (Region 0) and state into the chamber.
		// We avoid io.Copy because Value.Write treats any byte slice as raw user stream
		// and would attempt to tokenize the binary frame structure sequentially.
		acc := int(core.Cfg.StateAccumulator)
		if acc < 0 || acc >= primitive.Words {
			return fmt.Errorf(
				"visualizer: StateAccumulator %d out of bounds for value (%d words)",
				core.Cfg.StateAccumulator, primitive.Words,
			)
		}
		for j := 0; j <= acc; j++ {
			(*chamber)[j] = (*incoming)[j]
		}

		srv.Broadcast(telemetry.Event{
			Component: "Substrate",
			Action:    "Step",
			Data: telemetry.EventData{
				Stage:       "chamber-after",
				Frame:       i,
				Message:     telemetry.HumanDescribeValue(chamber),
				Instruction: telemetry.TruthOpName(uint8(telemetry.ReadVMInstruction() & 0xF)),
				DataPop:     cpu.Popcount(chamber, 0, int(core.Cfg.TokenBits)),
				OperandPop:  cpu.Popcount(chamber, int(core.Cfg.AffinityIndex), 64),
				AffinityPop: cpu.Popcount(chamber, int(core.Cfg.ProgramIndex), 64),
			},
		})

		srv.Broadcast(telemetry.Event{
			Component: "Substrate",
			Action:    "Step",
			Data: telemetry.EventData{
				Stage:       "kernel",
				Frame:       i,
				Message:     telemetry.HumanDescribeValue(chamber) + " · cpu.Backend: physics engine (affinity + program execution)",
				Instruction: telemetry.TruthOpName(uint8(telemetry.ReadVMInstruction() & 0xF)),
				DataPop:     cpu.Popcount(chamber, 0, int(core.Cfg.TokenBits)),
				OperandPop:  cpu.Popcount(chamber, int(core.Cfg.AffinityIndex), 64),
				AffinityPop: cpu.Popcount(chamber, int(core.Cfg.ProgramIndex), 64),
			},
		})

		mutatedFrame := make([]byte, primitive.ByteSize)
		if err := primitive.ValueToBytes(chamber, mutatedFrame); err != nil {
			return err
		}
		srv.BroadcastValueFrame(mutatedFrame)

		instr := uint8(telemetry.ReadVMInstruction() & 0xF)
		pressure := cpu.Popcount(chamber, int(core.Cfg.ProgramIndex), 64)

		srv.Broadcast(telemetry.Event{
			Component: "Substrate",
			Action:    "Step",
			Data: telemetry.EventData{
				Stage:       "state",
				Frame:       i,
				Message:     telemetry.HumanDescribeValue(chamber),
				ChunkText:   telemetry.ASCIIFramePreview(mutatedFrame, 120),
				Instruction: telemetry.TruthOpName(instr),
				DataPop:     cpu.Popcount(chamber, 0, int(core.Cfg.TokenBits)),
				OperandPop:  cpu.Popcount(chamber, int(core.Cfg.AffinityIndex), 64),
				AffinityPop: cpu.Popcount(chamber, int(core.Cfg.ProgramIndex), 64),
				Density:     float64(pressure) / regionBitWidth, // normalised instruction-region density
			},
		})

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.StepDelay):
		}
	}

	srv.Broadcast(telemetry.Event{
		Component: "Substrate",
		Action:    "Run",
		Data: telemetry.EventData{
			Stage:   "complete",
			Message: fmt.Sprintf("finished %d frames", framesDone),
		},
	})

	return nil
}

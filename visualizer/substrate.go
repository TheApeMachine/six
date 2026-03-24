package visualizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

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

	if opts.StepDelay <= 0 {
		opts.StepDelay = 50 * time.Millisecond
	}

	if opts.Repo == "" {
		opts.Repo = "facebook/babi_qa"
	}

	if opts.Subset == "" {
		opts.Subset = "en-10k-qa1"
	}

	if opts.TextColumn == "" {
		opts.TextColumn = "story"
	}

	backend := cpu.NewBackend()
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
				Instruction: telemetry.TruthOpName(uint8(cpu.ReadRegion(chamber, cpu.RegionInstruction) & 0xF)),
				DataPop:     cpu.Popcount(chamber, 0, primitive.DataBits),
				OperandPop:  cpu.Popcount(chamber, primitive.OperandStart, primitive.OperandBits),	
			},
		})

		if _, err := io.Copy(chamber, incoming); err != nil {
			srv.Broadcast(telemetry.Event{
				Component: "Substrate",
				Action:    "Step",
				Data: telemetry.EventData{
					Stage:   "chamber-copy-error",
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
				Stage:       "chamber-after",
				Frame:       i,
				Message:     telemetry.HumanDescribeValue(chamber),
				Instruction: telemetry.TruthOpName(uint8(cpu.ReadRegion(chamber, cpu.RegionInstruction) & 0xF)),
				DataPop:     cpu.Popcount(chamber, 0, primitive.DataBits),
				OperandPop:  cpu.Popcount(chamber, primitive.OperandStart, primitive.OperandBits),	
			},
		})

		if _, err := io.Copy(backend, chamber); err != nil {
			srv.Broadcast(telemetry.Event{
				Component: "Substrate",
				Action:    "Step",
				Data: telemetry.EventData{
					Stage:   "backend-error",
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
				Stage:       "kernel",
				Frame:       i,
				Message:     telemetry.HumanDescribeValue(chamber) + " · cpu.Backend: motor (Accumulate) + ALU on operand pressure",
				Instruction: telemetry.TruthOpName(uint8(cpu.ReadRegion(chamber, cpu.RegionInstruction) & 0xF)),
				DataPop:     cpu.Popcount(chamber, 0, primitive.DataBits),
				OperandPop:  cpu.Popcount(chamber, primitive.OperandStart, primitive.OperandBits),	
			},
		})

		mutatedFrame := make([]byte, primitive.ByteSize)
		if _, err := io.ReadFull(backend, mutatedFrame); err != nil {
			srv.Broadcast(telemetry.Event{
				Component: "Substrate",
				Action:    "Step",
				Data: telemetry.EventData{
					Stage:   "backend-read-error",
					Message: err.Error(),
					Frame:   i,
				},
			})

			return err
		}

		chamber = primitive.BytesToValue(mutatedFrame)

		instr := uint8(cpu.ReadRegion(chamber, cpu.RegionInstruction) & 0xF)
		pressure := cpu.Popcount(chamber, primitive.StateStart, primitive.StateBits)

		srv.Broadcast(telemetry.Event{
			Component: "Substrate",
			Action:    "Step",
			Data: telemetry.EventData{
				Stage:       "state",
				Frame:       i,
				Message:     telemetry.HumanDescribeValue(chamber),
				ChunkText:   telemetry.ASCIIFramePreview(mutatedFrame, 120),
				Instruction: telemetry.TruthOpName(instr),
				DataPop:     cpu.Popcount(chamber, 0, primitive.DataBits),
				OperandPop:  cpu.Popcount(chamber, primitive.OperandStart, primitive.OperandBits),
				Density:     float64(pressure) / float64(primitive.StateBits),
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

package vm

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/cluster"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
Machine provides a unified stream processing pipeline. All machine options are
applied during construction so tests and callers can instantiate isolated
machines without relying on package-level state.
*/
type Machine struct {
	ctx          context.Context
	cancel       context.CancelFunc
	err          error
	backend      *compute.Backend
	sources      io.Reader
	feedback     io.Writer
	destinations io.Writer
	output       io.ReadWriter
	controlplane *cluster.ControlPlane
	tokenizer    *Tokenizer

	// ingressActive is set once start() launches the ingress goroutine.
	// Machine.Read checks this to prevent a second concurrent consumer
	// from racing on machine.sources.
	ingressActive atomic.Bool
}

type machineOption func(*Machine)

/*
NewMachine constructs a new Machine with the provided options.
It requires a context for lifecycle management and will return
an error if the context is invalid or if the underlying stream
fails to start. The machine can be configured with various options,
such as custom datasets, stream adapters, and region counts.
*/
func NewMachine(
	ctx context.Context, opts ...machineOption,
) (machine *Machine, err error) {
	ctx, cancel := context.WithCancel(ctx)

	controlplane := cluster.NewControlPlane(ctx)

	machine = &Machine{
		ctx:          ctx,
		cancel:       cancel,
		controlplane: controlplane,
	}

	// Create backend with emit callback so signal-emitted children get
	// inserted into the spatial index automatically.
	machine.backend = compute.NewBackend(ctx, compute.WithEmitCallback(
		func(value *primitive.Value) {
			if controlplane == nil || value == nil {
				return
			}
			key := value.GetWord(core.Cfg.Value.Region.Affinity.Start)
			controlplane.Insert(key, *value)
		},
	))

	tokenizer, err := NewTokenizer(
		ctx,
		TokenizerWithStore(machine.controlplane),
		TokenizerWithBackend(machine.backend),
	)

	if err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, err),
		)
	}

	machine.sources = tokenizer
	machine.tokenizer = tokenizer
	machine.destinations = tokenizer

	for _, opt := range opts {
		opt(machine)
	}

	if machine.err = validate.Require(map[string]any{
		"ctx":          machine.ctx,
		"cancel":       machine.cancel,
		"sources":      machine.sources,
		"destinations": machine.destinations,
		"backend":      machine.backend,
		"tokenizer":    machine.tokenizer,
	}); machine.err != nil {
		return nil, errnie.Error(
			NewMachineError(ErrNotValidated, machine.err),
		)
	}

	machine.start()

	return machine, nil
}

/*
start launches the ingress goroutine that reads tokenized frames from the
tokenizer ring buffer and queues them for backend execution.

IMPORTANT: this is a one-way read loop, NOT an io.Copy feedback loop.
Frames are NOT written back to the tokenizer — that caused an infinite
recirculation loop where every frame was endlessly re-ingested at
memory speed. Recirculation is handled exclusively by the backend's
handleFollowUp mechanism (fw register → PRIORITY queue).
*/
func (machine *Machine) start() (err error) {
	machine.ingressActive.Store(true)
	buf := make([]byte, core.Cfg.Value.Bytes)

	go func() {
		defer machine.ingressActive.Store(false)

		for {
			select {
			case <-machine.ctx.Done():
				return
			default:
				n, readErr := machine.sources.Read(buf)
				if n > 0 {
					value := primitive.BytesToValue(buf[:n])

					valueID := value.GetWord(core.Cfg.Value.Region.ID.Start)
					telemetry.Emit(telemetry.Event{
						Component: "Machine",
						Action:    "Pipeline",
						Data: telemetry.EventData{
							Stage:   "queue",
							Message: "frame from tokenizer → backend queue",
							NodeID:  valueID,
						},
					})

					if queueErr := machine.backend.Queue(unsafe.Pointer(value)); queueErr != nil {
						errnie.Error(queueErr)
						continue
					}

					if value.IsPrompt() {
						// Offload prompt settle + output collection to a
						// separate goroutine so the read loop stays responsive
						// to new ingress frames.
						promptValue := value
						promptID := valueID
						go func() {
							telemetry.Emit(telemetry.Event{
								Component: "Machine",
								Action:    "Pipeline",
								Data: telemetry.EventData{
									Stage:   "prompt-start",
									Message: "waiting for prompt settle",
									NodeID:  promptID,
								},
							})

							output := machine.collectPromptOutput(promptValue)

							if machine.output != nil {
								if _, writeErr := machine.output.Write(output); writeErr != nil {
									errnie.Error(writeErr)
								}
							}
						}()
					}
				}

				if readErr != nil {
					if errors.Is(readErr, io.EOF) {
						continue
					}
					if machine.ctx.Err() != nil {
						return
					}
					machine.err = errnie.Error(
						NewMachineError(ErrStreamFailed, readErr),
					)
					return
				}
			}
		}
	}()

	return nil
}

/*
Read implements io.Reader for external consumers that need to pull
processed frames. The main ingress pipeline no longer uses this method
(see start()). This remains for backward-compat with callers that treat
Machine as an io.Reader.

IMPORTANT: Read returns an error if the ingress goroutine is active,
because both would consume from machine.sources and lose frames.
*/
func (machine *Machine) Read(p []byte) (n int, err error) {
	if machine.ingressActive.Load() {
		return 0, errors.New(
			"vm.Machine.Read: ingress goroutine is active; " +
				"use machine.output or WithOutput to consume results",
		)
	}

	select {
	case <-machine.ctx.Done():
		return 0, machine.ctx.Err()
	default:
		return machine.sources.Read(p)
	}
}

/*
collectPromptOutput walks the NextID chain from a prompt Value, decoding
tokenIDs from each linked Value in the chain. This is how the substrate
returns results: the prompt's program (and signal emission) link it to
relevant Values, and walking that chain produces the output.

If the chain is empty (NextID == 0), falls back to decoding the prompt's
own tokenIDs as a baseline.
*/
func (machine *Machine) collectPromptOutput(value *primitive.Value) []byte {
	if value == nil {
		return nil
	}

	valueID := value.GetWord(core.Cfg.Value.Region.ID.Start)
	nextID := value.GetWord(core.Cfg.Value.Region.Next.Start)

	// Walk the NextID chain to collect output from linked Values.
	if nextID != 0 && machine.controlplane != nil {
		var output []byte
		cursor := nextID
		seen := map[uint64]bool{valueID: true} // don't revisit prompt itself

		for cursor != 0 {
			if seen[cursor] {
				break // cycle guard
			}
			seen[cursor] = true

			// Try to decode this linked Value's tokenIDs. Values created
			// via NewValue have cached affine-mapped TokenIDs that
			// DecodeTokenIDs can reverse. Signal-emitted children don't
			// have those, so fall back to raw token region bytes.
			tokenIDs := primitive.ValueTokenIDsForLookup(cursor)
			if len(tokenIDs) == 0 {
				tokenIDs = machine.controlplane.LookupKeysByValueID(cursor)
			}

			if len(tokenIDs) > 0 {
				decoded := primitive.DecodeTokenIDs(tokenIDs)
				if len(decoded) > 0 {
					output = append(output, decoded...)
				}
			} else {
				// Signal children: their token region contains raw
				// HD-extracted bits, not affine-mapped TokenIDs. Read
				// the token region directly as observed bytes.
				frame, ok := machine.controlplane.FrameByValueID(cursor)
				if ok {
					frameVal := primitive.Value(frame)
					raw := frameVal.TokenRegionObservedBytes()
					if len(raw) > 0 {
						output = append(output, raw...)
					}
					cursor = frame[core.Cfg.Value.Region.Next.Start]
					continue
				}
			}

			// Follow the chain: look up the frame to read its NextID.
			frame, ok := machine.controlplane.FrameByValueID(cursor)
			if !ok {
				break
			}
			cursor = frame[core.Cfg.Value.Region.Next.Start]
		}

		if len(output) > 0 {
			resultPreview := string(output)
			if len(resultPreview) > 200 {
				resultPreview = resultPreview[:200]
			}
			telemetry.Emit(telemetry.Event{
				Component: "Machine",
				Action:    "Pipeline",
				Data: telemetry.EventData{
					Stage:      "prompt-complete",
					ResultText: resultPreview,
					NodeID:     valueID,
				},
			})
			return output
		}
	}

	// Fallback: decode the prompt's own tokenIDs (self-echo baseline).
	tokenIDs := primitive.ValueTokenIDsForLookup(valueID)
	if len(tokenIDs) == 0 && machine.controlplane != nil {
		tokenIDs = machine.controlplane.LookupKeysByValue(value)
	}
	if len(tokenIDs) == 0 && machine.controlplane != nil {
		tokenIDs = machine.controlplane.LookupKeysByValueID(valueID)
	}

	if len(tokenIDs) == 0 {
		telemetry.Emit(telemetry.Event{
			Component: "Machine",
			Action:    "Pipeline",
			Data: telemetry.EventData{
				Stage:   "prompt-empty",
				Message: "no linked Values and no token IDs for prompt",
				NodeID:  valueID,
			},
		})
		return []byte{}
	}

	output := primitive.DecodeTokenIDs(tokenIDs)
	if output == nil {
		output = []byte{}
	}

	telemetry.Emit(telemetry.Event{
		Component: "Machine",
		Action:    "Pipeline",
		Data: telemetry.EventData{
			Stage:   "prompt-fallback",
			Message: "no linked Values; decoded prompt's own tokenIDs",
			NodeID:  valueID,
		},
	})

	return output
}

/*
Write implements io.Writer so the machine acts as a wiring mechanism between
sources and destinations. Destinations can still aggregate outputs via
io.MultiWriter for fan-out.
*/
func (machine *Machine) Write(p []byte) (n int, err error) {
	if machine.destinations == nil {
		return len(p), nil
	}

	return machine.destinations.Write(p)
}

/*
Close implements io.Closer so the machine can be closed, which will cancel
the context, and if everything is wired up correctly, this should trigger
a full system-wide shutdown. This means that the system's context must be
the ultimate root context for the system.
*/
func (machine *Machine) Close() (err error) {
	if machine.cancel != nil {
		machine.cancel()
	}

	return err
}

/*
WithSources configures the machine with one or more sources, which act as
the ingress points for data.
*/
func WithSources(readers ...io.Reader) machineOption {
	return func(machine *Machine) {
		machine.sources = io.MultiReader(
			append([]io.Reader{machine.sources}, readers...)...,
		)
	}
}

/*
WithFeedback configures an additional prompt-output sink.
Prompt Values can be written to this output to build an explicit feedback
ingress path.
*/
func WithFeedback(writers ...io.Writer) machineOption {
	return func(machine *Machine) {
		if len(writers) == 0 {
			return
		}

		machine.feedback = io.MultiWriter(
			append([]io.Writer{}, writers...)...,
		)
	}
}

/*
WithDestinations configures the machine with one or more destinations,
which act as the egress points for data.
*/
func WithDestinations(writers ...io.Writer) machineOption {
	return func(machine *Machine) {
		machine.destinations = io.MultiWriter(
			append([]io.Writer{machine.destinations}, writers...)...,
		)
	}
}

/*
WithOutput configures the machine with an output reader, which is used to
capture the output of the machine.
*/
func WithOutput(rw io.ReadWriter) machineOption {
	return func(machine *Machine) {
		machine.output = rw
	}
}

package vm

import (
	"context"
	"errors"
	"math/bits"
	"math/rand"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
Machine is a central orchestrator that moves Values through a
processing pipeline. It should not try and control the process
it just routes Values between the different components of the system.
*/
type Machine struct {
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	host      *network.Host
	tokenizer *Tokenizer
	backend   *compute.Backend
	telemetry *telemetry.Bridge
	community []*primitive.Value

	communitySeeds map[uint64][primitive.AffinityWords]uint64
	communityFolds map[uint64][primitive.AffinityWords]uint64
	nextCommunity  uint64
}

type machineOpts func(*Machine)

func NewMachine(
	ctx context.Context, opts ...machineOpts,
) (*Machine, error) {
	ctx, cancel := context.WithCancel(ctx)

	bridge, err := telemetry.NewBridge(ctx, core.Cfg.TelemetryWebSocketURL)

	if err != nil {
		cancel()
		return nil, errnie.Error(err)
	}

	machine := &Machine{
		ctx:            ctx,
		cancel:         cancel,
		telemetry:      bridge,
		backend:        compute.NewBackend(ctx),
		communitySeeds: make(map[uint64][primitive.AffinityWords]uint64),
		communityFolds: make(map[uint64][primitive.AffinityWords]uint64),
		nextCommunity:  1,
	}

	for _, opt := range opts {
		opt(machine)
	}

	go func() {
		_ = bridge.Connect()
	}()

	if machine.host, machine.err = network.NewHost(ctx); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	if machine.tokenizer, machine.err = NewTokenizer(
		ctx,
	); machine.err != nil {
		return nil, errnie.Error(machine.err)
	}

	return machine, validate.Require(map[string]any{
		"ctx":       machine.ctx,
		"cancel":    machine.cancel,
		"host":      machine.host,
		"tokenizer": machine.tokenizer,
		"backend":   machine.backend,
	})
}

/*
Close the machine.
*/
func (machine *Machine) Close() error {
	var errs []error

	machine.cancel()

	if machine.host != nil {
		if err := machine.host.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.tokenizer != nil {
		if err := machine.tokenizer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if machine.backend != nil {
		if err := machine.backend.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

/*
Error returns the error of the machine.
*/
func (machine *Machine) Error() error {
	return machine.err
}

/*
Cycle executes the entire community loop until it converges (all continuations are 0)
or at least one newly resolved Value emerges.
*/
func (machine *Machine) Cycle() (resolved []*primitive.Value, err error) {
	for {
		select {
		case <-machine.ctx.Done():
			return nil, machine.ctx.Err()
		default:
			// 0. Route Values into Community Fields based on Affinity
			communities := make(map[uint64][]*primitive.Value)
			for _, value := range machine.community {
				// If a Value has computed an affinity, it migrates to that community
				aff := value.AffinityArray()
				
				// Check if affinity is non-zero
				isZero := true
				for _, w := range aff {
					if w != 0 {
						isZero = false
						break
					}
				}

				if !isZero {
					bestDist := 9999
					bestCID := uint64(0)
					
					for cID, seed := range machine.communitySeeds {
						dist := primitive.AffinityHamming(aff, seed)
						if dist < bestDist {
							bestDist = dist
							bestCID = cID
						}
					}
					
					var cID uint64
					if bestCID != 0 && bestDist <= core.Cfg.System.RouteBudget {
						// Found a community within budget.
						// Now check if it has reached the Shannon Limit (saturation via XOR fold).
						fold := machine.communityFolds[bestCID]
						
						// Compute XOR fold with the new value's affinity
						var newFold [primitive.AffinityWords]uint64
						popcount := 0
						for i := 0; i < primitive.AffinityWords; i++ {
							newFold[i] = fold[i] ^ aff[i]
							popcount += bits.OnesCount64(newFold[i])
						}
						
						// The affinity region is 257 bits.
						saturation := float64(popcount) / 257.0
						
						if saturation >= core.Cfg.System.ShannonLimit {
							// Saturated: spawn new community
							cID = machine.nextCommunity
							machine.nextCommunity++
							machine.communitySeeds[cID] = aff
							machine.communityFolds[cID] = aff
						} else {
							// Join existing and update fold
							cID = bestCID
							machine.communityFolds[cID] = newFold
						}
					} else {
						// Spawn new community
						cID = machine.nextCommunity
						machine.nextCommunity++
						machine.communitySeeds[cID] = aff
						machine.communityFolds[cID] = aff
					}

					value.SetProperty(primitive.COMMUNITY, cID)
				}

				cID, _ := value.Property(primitive.COMMUNITY)
				communities[cID] = append(communities[cID], value)
			}

			// 1. Scheduler: Build the active queue per community based on Continuation
			activeCommunities := make(map[uint64][]*primitive.Value)

			for cID, comm := range communities {
				var active []*primitive.Value
				for _, value := range comm {
					cont := value.SchedulingNext()

					// Handle autonomous reprogramming via CONTINUATION
					if cont > 0 && cont <= 20 {
						var fw core.FirmwareType
						switch cont {
						case 1:
							fw = core.FOLD_SUBSTRATE
						case 2:
							fw = core.CAUSAL_EXPLORE
						case 3:
							fw = core.VOTE_SWARM
						}

						if fw != "" {
							value.InstallFirmware(fw)
							value.SetProperty(primitive.STATUS, 0)
							value.SetProperty(primitive.CONTINUATION, value.ID()) // Re-enter queue with new program
							active = append(active, value)
							continue
						}
					} else if cont != 0 {
						// We only implement "continuation = own id" for now.
						active = append(active, value)
					}
				}
				if len(active) > 0 {
					activeCommunities[cID] = active
				}
			}

			// 2. Encounter / Staging Substrate & Execution per Community
			for cID, active := range activeCommunities {
				comm := communities[cID]
				nComm := len(comm)

				if nComm > 1 {
					// Each active Value encounters another random Value from its own community
					// and stages B's context and gradient into A's asset region.
					for _, a := range active {
						bIdx := rand.Intn(nComm)
						b := comm[bIdx]

						if a == b {
							bIdx = (bIdx + 1) % nComm
							b = comm[bIdx]
						}

						// Stage B's Context (words 40-47) into A's Asset[8..15] (words 80-87)
						// Stage B's Gradient (words 48-55) into A's Asset[16..23] (words 88-95)
						for i := 0; i < 8; i++ {
							a.Set(80+i, (*b)[40+i]) // Context
							a.Set(88+i, (*b)[48+i]) // Gradient
						}
					}
				}

				spawned := machine.backend.ExecuteCommunity(active)
				if len(spawned) > 0 {
					machine.community = append(machine.community, spawned...)
				}
			}

			done := len(activeCommunities) == 0
			var newlyResolved []*primitive.Value

			for _, value := range machine.community {
				status, _ := value.Property(primitive.STATUS)
				if status == uint64(primitive.RESOLVED) {
					newlyResolved = append(newlyResolved, value)
				}

				if machine.telemetry != nil {
					machine.telemetry.Write(value.Bytes())
				}
			}

			if len(newlyResolved) > 0 {
				return newlyResolved, nil
			}

			if done {
				return nil, nil
			}
		}
	}
}

/*
Load walks Generate(), mints Morton-packed Values from each sample’s Text via
primitive.NewValue (see tokenizer.IngestSample), stamps every segment’s
Properties word when Label is present, then runs Cycle.
*/
func (machine *Machine) Load(dataset data.Provider) (err error) {
	if err := validate.Require(map[string]any{
		"tokenizer": machine.tokenizer,
	}); err != nil {
		return errnie.Error(err)
	}

	var segments []*primitive.Value

	for sample := range dataset.Generate() {
		if segments, err = machine.tokenizer.IngestSample(
			machine.ctx, sample,
		); err != nil {
			return errnie.Error(err)
		}

		machine.community = append(machine.community, segments...)

		if _, err := machine.Cycle(); err != nil {
			return errnie.Error(err)
		}
	}

	return nil
}

/*
Prompt injects the prompt segment Values into the community and cycles until settled.
*/
func (machine *Machine) Prompt(values ...*primitive.Value) (
	resolved []*primitive.Value, err error,
) {
	if err := validate.Require(map[string]any{
		"values": values,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	for _, value := range values {
		if value == nil {
			continue
		}
		value.SetProperty(primitive.ROLE, uint64(primitive.ValueRolePrompt))
		machine.community = append(machine.community, value)
	}

	return machine.Cycle()
}

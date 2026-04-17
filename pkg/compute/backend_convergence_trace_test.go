package compute

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
regionSnapshot captures the four ALU-visible regions in hex so the trace
reads like the visualiser's inspect panel. Words are sliced with the
runtime layout declared in cmd/cfg/config.yml — properties spans 48..71
and asset spans 72..119 — so the trace labels match what StageAssetFrom
and the config-driven rules actually read. The call site is a test, so
the zero-alloc guidelines do not apply — legibility wins.
*/
type regionSnapshot struct {
	signals    []uint64
	context    []uint64
	gradient   []uint64
	properties []uint64
	asset      []uint64
	scheduling uint64
	ttl        uint64
	id         uint64
}

/*
takeSnapshot reads the four gossip-visible regions plus scheduling/TTL so
the test's failure output doubles as a diagnostic trace. The Value is not
read through Value.Read here — that path re-encodes for the wire, which
would hide the raw word layout the visualiser is arguing about. The
sliced properties/asset windows match the extended-band layout in
config.yml (properties 48..71, asset 72..119); the snapshot captures the
first eight words of each so the dump stays human-readable while still
covering every slot referenced by the rule evaluator (asset[0,8] peer
signals, asset[8,8] peer context, asset[16,8] peer gradient, etc.).
*/
func takeSnapshot(value *primitive.Value) regionSnapshot {
	words := (*[128]uint64)(value)

	slice := func(start, span int) []uint64 {
		out := make([]uint64, span)
		copy(out, words[start:start+span])
		return out
	}

	return regionSnapshot{
		signals:    slice(24, 8),
		context:    slice(32, 8),
		gradient:   slice(40, 8),
		properties: slice(48, 8),
		asset:      slice(72, 8),
		scheduling: words[kernel.SchedulingNextProgramWord],
		ttl:        words[kernel.PropertiesTTLWord],
		id:         value.ID(),
	}
}

/*
printSnapshot formats a snapshot with t.Logf so -v shows the full region
layout per pass. Doing this as a test helper keeps the diagnostic output
alongside the assertions — if a future refactor breaks convergence, the
failing test prints the same panel the user stares at in the HUD. The
base offsets passed to dump match the runtime layout (asset at w72) so
the log labels line up with the rule-evaluator condition keys instead of
the legacy narrow-properties offsets.
*/
func printSnapshot(t *testing.T, label string, snap regionSnapshot) {
	t.Helper()
	t.Logf("── %s ──  id=%d  scheduling(w117)=0x%016x  ttl(w51)=0x%016x", label, snap.id, snap.scheduling, snap.ttl)
	dump := func(name string, base int, words []uint64) {
		for idx, word := range words {
			t.Logf("  %s w%-3d 0x%016x", name, base+idx, word)
		}
	}
	dump("SIGNALS   ", 24, snap.signals)
	dump("CONTEXT   ", 32, snap.context)
	dump("GRADIENT  ", 40, snap.gradient)
	dump("PROPERTIES", 48, snap.properties)
	dump("ASSET[0,8]", 72, snap.asset)
}

/*
TestBeamSwarmConvergenceTrace runs the current beam_swarm_step program on
a clean Value, dumps the four gossip regions before and after the firmware
pass, and then exercises a short run of resident heartbeats. The diagnostic
intent is unchanged — each pass's region dump is logged so a future
regression prints the same panel the user stares at in the HUD — but the
assertions now match the deployed DSL: beam_swarm_step's trailing
`signals[0,8] gradient[0,8] gradient[0,8] xor accumulate` folds every
scored prediction-error frame back into gradient, so gradient MUST move
after one pass. A zero gradient here is a regression of the causal belief
update; the system would otherwise be stuck replaying the same rotation
against a frozen belief direction and neither the visualiser nor any
downstream causal-modelling program would see the substrate evolve.
*/
func TestBeamSwarmConvergenceTrace(t *testing.T) {
	Convey("Given a clean Value with varied tokens and the beam_swarm_step DSL", t, func() {
		backend := NewBackend(context.Background())
		So(backend, ShouldNotBeNil)

		value := primitive.AllocValue()
		So(value, ShouldNotBeNil)
		defer value.Close()

		value.StampNewID()

		// Stamp tokens with distinctive, non-degenerate data so the DSL has
		// real entropy to XOR against. Using a permutation of uint64s avoids
		// the all-zero path where the ALU short-circuits to zero output.
		for idx := 0; idx < 16; idx++ {
			(*value)[idx] = uint64(0x0123456789abcdef) ^ (uint64(idx+1) * 0x9e3779b97f4a7c15)
		}

		// Give the Value a non-trivial TTL so ApplyPostExecutionLifecycle
		// decrements instead of idling. The visualiser sees TTL decrementing
		// in the wild, so match that.
		(*value)[kernel.PropertiesTTLWord] = 250

		// The source mirrors the deployed beam_swarm_step in cmd/cfg/config.yml
		// byte-for-byte, including the trailing gradient fold. Keeping the
		// test source inline (instead of round-tripping through viper) lets
		// the test run without a config file on disk, while the explicit
		// quoting documents the line that turned gradient from a read-only
		// window into a live belief lane.
		beamSource := "" +
			"tokens[0,8]     gradient[0,8]   context[0,8]    xor accumulate\n" +
			"tokens[0,8]     context[0,8]    signals[0,8]    xor accumulate\n" +
			"signals[0,8]    signals[0,8]    properties[0,1] xor reduce\n" +
			"properties[0,1] properties[1,1] properties[0,1] or  accumulate\n" +
			"properties[0,1] affinity[0,5]   affinity[0,5]   xor accumulate\n" +
			"signals[0,8]    gradient[0,8]   gradient[0,8]   xor accumulate\n" +
			"next self\n"

		executable := programmer.NewExecutable(value, beamSource, nil)
		So(executable, ShouldNotBeNil)

		Convey("The pre-execution snapshot should show zero regions", func() {
			before := takeSnapshot(value)
			printSnapshot(t, "BEFORE firmware chain", before)

			for _, word := range before.signals {
				So(word, ShouldEqual, uint64(0))
			}
			for _, word := range before.context {
				So(word, ShouldEqual, uint64(0))
			}
			for _, word := range before.gradient {
				So(word, ShouldEqual, uint64(0))
			}
		})

		Convey("After the firmware chain runs, gradient must move and scheduling must loop", func() {
			backend.Dispatch(executable)

			afterChain := takeSnapshot(value)
			printSnapshot(t, "AFTER firmware chain (1 full pass)", afterChain)

			// With the trailing gradient fold in place the belief lane is
			// written by the prediction-error signature every pass. A
			// non-zero gradient here is the in-band witness that the
			// active-inference update is live; a zero gradient would be
			// the regression the whole causal-modelling deployment hinges on.
			var gradientMoved bool

			for idx, word := range afterChain.gradient {
				if word != 0 {
					gradientMoved = true
					t.Logf("gradient moved at w%d = 0x%016x", 40+idx, word)
				}
			}

			So(gradientMoved, ShouldBeTrue)
			So(afterChain.scheduling, ShouldEqual, value.ID())
		})

		Convey("Running 20 resident heartbeats keeps gradient evolving so the system has pressure to move", func() {
			backend.Dispatch(executable)

			var prev regionSnapshot
			var gradientChanges int

			for pass := 1; pass <= 20; pass++ {
				resident := programmer.NewResidentExecutable(value)
				backend.Dispatch(resident)

				snap := takeSnapshot(value)
				label := fmt.Sprintf("heartbeat pass %d", pass)
				printSnapshot(t, label, snap)

				if pass > 1 && !signalsEqual(prev.gradient, snap.gradient) {
					gradientChanges++
				}

				prev = snap
			}

			// Even if signals/context reach a 2-cycle attractor under pure
			// self-XOR, the gradient fold layers the prediction error onto
			// the belief, so at least a handful of heartbeats must observe
			// gradient mutating. Without this, the visualiser's gradient
			// panel would remain a static block of zeros and every causal
			// modelling program that reads gradient as SrcA would run
			// against the same frozen input forever.
			t.Logf("gradient changed on %d of 19 heartbeat transitions", gradientChanges)
			So(gradientChanges, ShouldBeGreaterThan, 0)
		})
	})
}

func signalsEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}

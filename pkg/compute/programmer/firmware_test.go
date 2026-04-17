package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewFirmware constructs the rule-evaluator handle used by Next.
*/
func TestNewFirmware(t *testing.T) {
	Convey("When NewFirmware is called", t, func() {
		fw := NewFirmware()

		Convey("It should return a non-nil Firmware", func() {
			So(fw, ShouldNotBeNil)
		})
	})
}

/*
TestFirmware_HasBits scans uint64 words for any set bit.
*/
func TestFirmware_HasBits(t *testing.T) {
	Convey("Given a Firmware receiver", t, func() {
		var firmware Firmware

		Convey("HasBits should return false for nil or empty slices", func() {
			So(firmware.HasBits(nil), ShouldBeFalse)
			So(firmware.HasBits([]uint64{}), ShouldBeFalse)
		})

		Convey("HasBits should return true when any word is non-zero", func() {
			So(firmware.HasBits([]uint64{0, 1}), ShouldBeTrue)
		})
	})
}

/*
TestFirmware_Next selects firmware from value.rules when boolean conditions match region occupancy.
*/
func TestFirmware_Next(t *testing.T) {
	Convey("Given a fresh Value (affinity region all zero)", t, func() {
		values, err := primitive.NewValue([]byte("firmware next"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		firmware := NewFirmware()

		Convey("Next should return the link firmware when prev and next are empty", func() {
			next := firmware.Next(value)
			So(next, ShouldEqual, "link")
		})

		Convey("Next should return the affinity firmware when prev is set but next is empty", func() {
			prevStart, _ := primitive.PrevRegion.WordExtent()
			value.Set(prevStart, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "affinity")
		})

		Convey("Next should return the affinity firmware when next is set but prev is empty", func() {
			nextStart, _ := primitive.NextRegion.WordExtent()
			value.Set(nextStart, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "affinity")
		})

		Convey("Next should return the affinity firmware when both prev and next are set", func() {
			prevStart, _ := primitive.PrevRegion.WordExtent()
			nextStart, _ := primitive.NextRegion.WordExtent()
			value.Set(prevStart, 1)
			value.Set(nextStart, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "affinity")
		})

		Convey("Next should return beam_swarm_step after bootstrap when no behaviour rule supersedes it", func() {
			/*
			With link + affinity satisfied and no label / peer delivery /
			context fill yet, the explore rule matches: it's the default
			post-bootstrap behaviour and exists specifically so Values
			don't settle on `affinity` as their resident program.
			*/
			prevStart, _ := primitive.PrevRegion.WordExtent()
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			value.Set(prevStart, 1)
			value.Set(affinityStart, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "beam_swarm_step")
		})

		Convey("Next should return empty string once context has been populated", func() {
			/*
			After the first beam pass writes context[0,8], the explore
			rule stops matching and the Chain walk hands off to the
			resident program for in-place looping via `next self`.
			*/
			prevStart, _ := primitive.PrevRegion.WordExtent()
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			contextStart, _ := primitive.ContextRegion.WordExtent()
			value.Set(prevStart, 1)
			value.Set(affinityStart, 1)
			value.Set(contextStart, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "")
		})

		Convey("Next should return hypothesis when beam_swarm_step has settled a belief", func() {
			/*
			Post-explore steady state: affinity set, context and gradient
			carry a live belief, surprisal has reduced into properties[0,1],
			and signals[0,8] was populated by the beam's xor accumulate so
			both the classify gate (signals[0,1]: false) and the explore
			gate (signals[0,8]: false) have flipped off. With no refutation
			target armed, the hypothesize rule fires — this is the system
			autonomously asking "what if".
			*/
			prevStart, _ := primitive.PrevRegion.WordExtent()
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			contextStart, _ := primitive.ContextRegion.WordExtent()
			gradientStart, _ := primitive.GradientRegion.WordExtent()
			signalsStart, _ := primitive.SignalsRegion.WordExtent()
			propertiesStart, _ := primitive.PropertiesRegion.WordExtent()

			value.Set(prevStart, 1)
			value.Set(affinityStart, 1)
			value.Set(contextStart, 1)
			value.Set(gradientStart, 1)
			// signals[0,1] — beam's reduce wrote a popcount scalar here,
			// which also fills signals[0,8] so explore is disarmed and
			// classify's signals[0,1]: false gate flips.
			value.Set(signalsStart, 1)
			// properties[0,1] — accuracy/surprisal witness
			value.Set(propertiesStart, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "hypothesis")
		})

		Convey("Next should return falsification once a hypothesis target is armed", func() {
			/*
			After the hypothesis program stamps properties[1,1], the
			hypothesize rule disarms (gate is properties[1,1]: false) and
			the falsify rule takes over — the Popperian test of the
			claim. The kernel's ApplyRefutationProbe watches signals for
			a long one-run and decides whether the claim survives.
			*/
			prevStart, _ := primitive.PrevRegion.WordExtent()
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			signalsStart, _ := primitive.SignalsRegion.WordExtent()
			propertiesStart, _ := primitive.PropertiesRegion.WordExtent()

			value.Set(prevStart, 1)
			value.Set(affinityStart, 1)
			// signals[0,1] set so classify and explore disarm — NewValue
			// already populated tokens[0,16] from the input bytes.
			value.Set(signalsStart, 1)
			// properties[1,1] — refutation target armed
			value.Set(propertiesStart+1, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "falsification")
		})

		Convey("Next should return causal_hub once the kernel refutation witness has landed", func() {
			/*
			ApplyRefutationProbe stamps FalsifiedBitNoiseWord into
			properties[4,1] and clears properties[1,1]. That flips the
			iterate_causal gate on; rule-ordered before hypothesize so
			the Value commits to counterfactual iteration instead of
			immediately forming another hypothesis over the collapsed
			belief. context + signals must be populated so the explore
			rule doesn't shadow iterate_causal.
			*/
			prevStart, _ := primitive.PrevRegion.WordExtent()
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			contextStart, _ := primitive.ContextRegion.WordExtent()
			gradientStart, _ := primitive.GradientRegion.WordExtent()
			signalsStart, _ := primitive.SignalsRegion.WordExtent()
			propertiesStart, _ := primitive.PropertiesRegion.WordExtent()

			value.Set(prevStart, 1)
			value.Set(affinityStart, 1)
			value.Set(contextStart, 1)
			value.Set(gradientStart, 1)
			value.Set(signalsStart, 1)
			// properties[4,1] — kernel's falsification witness
			value.Set(propertiesStart+4, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "causal_hub")
		})

		Convey("Next should return intervene for a foreign carrier with severed history", func() {
			/*
			A do-operation carrier arrives via Conn.Write: StageAssetFrom
			lands the peer's gradient into asset[16,8], the carrier has
			affinity from its home community, prev is explicitly zero
			(severed history), and properties[0,1] is still zero because
			the intervention hasn't been scored yet. Ruled before
			peer_gap so severed-history carriers take the do-op path.
			*/
			affinityStart, _ := primitive.AffinityRegion.WordExtent()
			contextStart, _ := primitive.ContextRegion.WordExtent()
			assetStart, _ := primitive.AssetRegion.WordExtent()
			nextStart, _ := primitive.NextRegion.WordExtent()

			value.Set(affinityStart, 1)
			value.Set(contextStart, 1)
			// asset[16,8] — peer gradient staged by StageAssetFrom
			value.Set(assetStart+16, 1)
			// next non-zero keeps us out of the link rule (which
			// requires both prev and next to be empty).
			value.Set(nextStart, 1)

			next := firmware.Next(value)
			So(next, ShouldEqual, "intervene")
		})

		Reset(func() {
			value.Close()
		})
	})
}

/*
stubScheduler is an inline Scheduler — Submit runs the task and Finalizes
the produced Executable on the caller's goroutine. That collapses the
"pool dispatch → Backend.Dispatch → Finalize" loop to pure function calls
so the test can measure terminal-finalizer propagation without lighting
up a compute.Backend (which would drag in pool and its runtime-linkname
constraints at link time). The ALU pass is faked by mutating the Value's
rule-gate regions after each firmware observation so the rule evaluator
actually progresses on re-entry — without this, link would match forever
because no region state changes in a stubbed environment.
*/
type stubScheduler struct {
	firmware *Firmware
	names    []string
}

func (stub *stubScheduler) Submit(task func() *Executable) {
	if task == nil {
		return
	}

	executable := task()

	if executable == nil {
		return
	}

	name := executable.Firmware()
	stub.names = append(stub.names, name)

	// Simulate the ALU's effect on rule-gate regions so the recursive
	// Chain call sees the next rule fire. The real substrate would
	// write these bits as part of the firmware's program; the test
	// cares about the scheduler wiring, not the ALU outputs.
	value := executable.Value()

	switch name {
	case "link":
		prevStart, _ := primitive.PrevRegion.WordExtent()
		value.Set(prevStart, 1)
	case "affinity":
		affinityStart, _ := primitive.AffinityRegion.WordExtent()
		value.Set(affinityStart, 1)
	case "beam_swarm_step":
		// The real beam program's first frame populates context[0,8];
		// the stub mirrors that single word so the explore rule's
		// context-empty gate flips and the chain advances to resident.
		contextStart, _ := primitive.ContextRegion.WordExtent()
		value.Set(contextStart, 1)
	}

	executable.Finalize()
}

/*
TestFirmware_Chain covers the rule-walking scheduler entry point: first
hop fires the matching firmware, the attached finalizer re-submits into
Chain so the Value walks every rule until steady state, and the optional
terminal Finalizer runs exactly once on the resident Executable.
*/
func TestFirmware_Chain(t *testing.T) {
	Convey("Given a fresh Value that triggers the link rule", t, func() {
		values, err := primitive.NewValue([]byte("chain test"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]

		defer value.Close()

		// Strip prev/next so the rule engine picks link first. NewValue
		// returns a single segment here so the stamps are already zero,
		// but we zero explicitly to pin the precondition.
		prevStart, prevWords := primitive.PrevRegion.WordExtent()
		nextStart, nextWords := primitive.NextRegion.WordExtent()
		affinityStart, affinityWords := primitive.AffinityRegion.WordExtent()

		for offset := 0; offset < prevWords; offset++ {
			value.Set(prevStart+offset, 0)
		}
		for offset := 0; offset < nextWords; offset++ {
			value.Set(nextStart+offset, 0)
		}
		for offset := 0; offset < affinityWords; offset++ {
			value.Set(affinityStart+offset, 0)
		}

		firmware := NewFirmware()

		Convey("Chain returns nil when any required input is nil", func() {
			So(firmware.Chain(nil, value), ShouldBeNil)

			scheduler := &stubScheduler{firmware: firmware}
			So(firmware.Chain(scheduler, nil), ShouldBeNil)

			var nilFirmware *Firmware
			So(nilFirmware.Chain(scheduler, value), ShouldBeNil)
		})

		Convey("When Chain is submitted with a terminal Finalizer", func() {
			scheduler := &stubScheduler{firmware: firmware}

			var terminalHits int
			var terminalValue *primitive.Value

			terminal := func(finalized *primitive.Value) {
				terminalHits++
				terminalValue = finalized
			}

			// Seed the first hop. The stub's Submit runs the task
			// synchronously and finalizes the resulting Executable,
			// which itself submits the next hop — so one Submit here
			// unrolls the whole rule walk.
			scheduler.Submit(func() *Executable {
				return firmware.Chain(scheduler, value, terminal)
			})

			Convey("It walks link → affinity → beam_swarm_step → resident", func() {
				So(scheduler.names, ShouldResemble, []string{"link", "affinity", "beam_swarm_step", ""})
			})

			Convey("The terminal Finalizer fires exactly once on the resident value", func() {
				So(terminalHits, ShouldEqual, 1)
				So(terminalValue, ShouldEqual, value)
			})
		})

		Convey("When Chain is submitted without a terminal", func() {
			scheduler := &stubScheduler{firmware: firmware}

			scheduler.Submit(func() *Executable {
				return firmware.Chain(scheduler, value)
			})

			Convey("It still walks through explore to resident and stops cleanly", func() {
				So(
					scheduler.names,
					ShouldResemble,
					[]string{"link", "affinity", "beam_swarm_step", ""},
				)
			})
		})

		Convey("When Chain starts at the affinity rule (prev is set)", func() {
			value.Set(prevStart, 1)

			scheduler := &stubScheduler{firmware: firmware}

			var hits int
			terminal := func(*primitive.Value) { hits++ }

			scheduler.Submit(func() *Executable {
				return firmware.Chain(scheduler, value, terminal)
			})

			Convey("It skips link, runs affinity, explores, then resident", func() {
				So(
					scheduler.names,
					ShouldResemble,
					[]string{"affinity", "beam_swarm_step", ""},
				)
				So(hits, ShouldEqual, 1)
			})
		})

		Convey("When Chain starts post-bootstrap (prev + affinity set)", func() {
			value.Set(prevStart, 1)
			value.Set(affinityStart, 1)

			scheduler := &stubScheduler{firmware: firmware}

			var hits int
			terminal := func(*primitive.Value) { hits++ }

			scheduler.Submit(func() *Executable {
				return firmware.Chain(scheduler, value, terminal)
			})

			Convey("It installs beam_swarm_step via explore and then lands on resident", func() {
				So(
					scheduler.names,
					ShouldResemble,
					[]string{"beam_swarm_step", ""},
				)
				So(hits, ShouldEqual, 1)
			})
		})

		Convey("When context is already populated the chain settles on resident", func() {
			/*
			This pins the other direction: a Value whose ALU has already
			filled context[0,8] (e.g. after the first beam pass) must
			fall through every behaviour rule and land on the resident
			Executable. That handoff is what lets `next self` keep the
			beam looping without re-triggering the rule walk.
			*/
			value.Set(prevStart, 1)
			value.Set(affinityStart, 1)

			contextStart, _ := primitive.ContextRegion.WordExtent()
			value.Set(contextStart, 1)

			signalsStart, _ := primitive.SignalsRegion.WordExtent()
			value.Set(signalsStart, 1)

			scheduler := &stubScheduler{firmware: firmware}

			var hits int
			terminal := func(*primitive.Value) { hits++ }

			scheduler.Submit(func() *Executable {
				return firmware.Chain(scheduler, value, terminal)
			})

			So(scheduler.names, ShouldResemble, []string{""})
			So(hits, ShouldEqual, 1)
		})
	})
}

func BenchmarkFirmware_HasBits(b *testing.B) {
	var firmware Firmware
	region := []uint64{0, 0, 0, 1}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = firmware.HasBits(region)
	}
}

func BenchmarkFirmware_Next(b *testing.B) {
	values, err := primitive.NewValue([]byte("bench firmware"))

	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}

	value := values[0]
	defer value.Close()

	fw := NewFirmware()

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = fw.Next(value)
	}
}

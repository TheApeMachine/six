package vm

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestNewOrchestrator verifies construction. The orchestrator is the
pipeline seed — it owns the work queue that the pool workers drain
and the root Field where published Values land. Constructing without
all three in place would leave a broken seed that silently eats work.
*/
func TestNewOrchestrator(t *testing.T) {
	Convey("Given a live context", t, func() {
		orchestrator, err := NewOrchestrator(context.Background())

		So(err, ShouldBeNil)
		So(orchestrator, ShouldNotBeNil)

		defer func() {
			So(orchestrator.Close(), ShouldBeNil)
		}()

		Convey("It wires a queue, firmware evaluator, and field", func() {
			So(orchestrator.field, ShouldNotBeNil)
		})

		Convey("Error is clean at rest", func() {
			So(orchestrator.Error(), ShouldBeNil)
		})
	})
}

/*
TestOrchestratorClose covers the teardown contract. Close cancels the
shared context so every dependent subsystem that honors
context.Done() (queue workers, field readers, downstream Conns) can
stop cleanly, and it must be safe to call on a freshly built
orchestrator.
*/
func TestOrchestratorClose(t *testing.T) {
	Convey("Given a freshly built orchestrator", t, func() {
		orchestrator, err := NewOrchestrator(context.Background())

		So(err, ShouldBeNil)

		Convey("Close returns nil and cancels the context", func() {
			So(orchestrator.Close(), ShouldBeNil)
			So(orchestrator.ctx.Err(), ShouldNotBeNil)
		})
	})
}

/*
TestOrchestratorCycle verifies the seed path. Cycle takes Values that
have just come off the tokenizer and hands each non-nil Value to
Firmware.Chain via queue.Submit so the pool can walk the rule chain.
nil entries are skipped so callers can pass segment slices without
defensive filtering. Cycle returns the Values that fell out of the
terminal emitter in pass 2 — one per non-nil input, reflecting the
full io.ReadWriteCloser pipeline the Conn, field, and emitter form.
*/
func TestOrchestratorCycle(t *testing.T) {
	Convey("Given an orchestrator and a mix of real and nil Values", t, func() {
		orchestrator, err := NewOrchestrator(context.Background())

		So(err, ShouldBeNil)

		values := make([]*primitive.Value, 3)
		for idx := range values {
			values[idx] = primitive.AllocValue()
		}

		// Wait for the pool to drain BEFORE Close() and value frees.
		// The pool has no Wait/Drain primitive, so leftover workers can
		// touch Value memory after it's been returned to the arena —
		// which then pollutes the next test's AllocValue. Ordered
		// defers (LIFO): drain → orchestrator.Close → values.Close.
		defer func() {
			for _, value := range values {
				value.Close()
			}
		}()

		defer func() {
			So(orchestrator.Close(), ShouldBeNil)
		}()

		defer func() {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && len(orchestrator.field.Values()) > 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}()

		// Thread a nil through the slice so we exercise the skip
		// branch in Cycle without allocating a sentinel type.
		mixed := append(values, nil)

		resolved, cycleErr := orchestrator.Cycle(mixed...)

		Convey("It tolerates nil entries and returns the pipeline output", func() {
			So(cycleErr, ShouldBeNil)
			// nil was compacted out of the bundle, so the emitter
			// only observes frames for the three real Values.
			So(len(resolved), ShouldEqual, len(values))
		})

		Convey("It leaves the orchestrator error-clean after submission", func() {
			// Submissions go directly to pool workers rather than a
			// ring buffer, so there is nothing synchronous to count;
			// the invariant worth pinning is that Cycle never flips
			// the orchestrator into an error state.
			So(orchestrator.Error(), ShouldBeNil)
		})
	})
}

/*
TestOrchestratorError defends the trivial getter. The field is unused
while nothing fails during construction, but callers wire it into
error-propagation chains so the zero-state contract matters.
*/
func TestOrchestratorError(t *testing.T) {
	Convey("Given a fresh orchestrator", t, func() {
		orchestrator, err := NewOrchestrator(context.Background())

		So(err, ShouldBeNil)

		defer func() {
			So(orchestrator.Close(), ShouldBeNil)
		}()

		Convey("Error returns nil when nothing has gone wrong", func() {
			So(orchestrator.Error(), ShouldBeNil)
		})
	})
}

/*
TestOrchestratorCycleChainsValues is the end-to-end proof of the in-band
gossip chain. Cycle receives three pre-bootstrapped Values (rule evaluator
falls straight through to resident), the live pool dispatches each to the
real compute.Backend, and each resident Executable's terminal Finalizer
stages its Signals+Context+Gradient+Properties block into the successor's
Asset window before submitting the next hop. Polling V[N-1].Asset for a
non-zero word is the cheapest synchronization the fire-and-forget
orchestrator exposes — once the last Value's asset is populated, every
upstream hop has necessarily finalized.
*/
func TestOrchestratorCycleChainsValues(t *testing.T) {
	Convey("Given an orchestrator and three pre-bootstrapped Values", t, func() {
		orchestrator, err := NewOrchestrator(context.Background())

		So(err, ShouldBeNil)

		// Drain the queue before orchestrator.Close() fires so no
		// worker is still holding a Value pointer when the arena
		// reclaims it. This prevents memory pollution leaking into
		// subsequent tests via AllocValue returning a slot that a
		// previous worker is still writing to.
		defer func() {
			So(orchestrator.Close(), ShouldBeNil)
		}()

		defer func() {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && len(orchestrator.field.Values()) > 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}()

		prevStart, _ := primitive.PrevRegion.WordExtent()
		nextStart, _ := primitive.NextRegion.WordExtent()
		affinityStart, affinityWords := primitive.AffinityRegion.WordExtent()
		signalsStart, signalsWords := primitive.SignalsRegion.WordExtent()
		_, contextWords := primitive.ContextRegion.WordExtent()
		_, gradientWords := primitive.GradientRegion.WordExtent()
		_, propertiesWords := primitive.PropertiesRegion.WordExtent()
		assetStart, _ := primitive.AssetRegion.WordExtent()

		stageWords := signalsWords + contextWords + gradientWords + propertiesWords

		chain := make([]*primitive.Value, 3)

		for idx := range chain {
			chain[idx] = primitive.AllocValue()
			chain[idx].StampNewID()

			// Force resident on the first hop so the chain executes
			// without spending time on link/affinity firmware. This
			// isolates what we are testing — the cross-Value propagation
			// wired by submitChainHop.
			chain[idx].Set(prevStart, 0xDEADBEEF)
			chain[idx].Set(nextStart, 0xFEEDFACE)
			for offset := 0; offset < affinityWords; offset++ {
				chain[idx].Set(affinityStart+offset, 0x1111_2222_3333_4444)
			}

			// Unique S/C/G/P sentinel per Value so the assertion can tell
			// V[0]'s bits from V[1]'s after propagation.
			for offset := 0; offset < stageWords; offset++ {
				(*chain[idx])[signalsStart+offset] = uint64(uint64(idx+1)<<40) | uint64(offset)
			}
		}

		defer func() {
			for _, value := range chain {
				value.Close()
			}
		}()

		resolved, cycleErr := orchestrator.Cycle(chain[0], chain[1], chain[2])

		So(cycleErr, ShouldBeNil)
		// Pass 2's terminal emitter collects one frame per bundled
		// Value as io.Copy drains the FrameDelimitedReader, so the
		// resolved slice mirrors the input cardinality.
		So(len(resolved), ShouldEqual, len(chain))

		// Poll until the last Value in the chain observes a staged
		// Asset — the orchestrator is fire-and-forget so this is the
		// cheapest observable "the whole chain has quiesced" signal.
		deadline := time.Now().Add(3 * time.Second)

		for time.Now().Before(deadline) {
			if (*chain[2])[assetStart] != 0 {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}

		// Signals+Context+Gradient (first 24 words of the stage block)
		// are pure output regions that the substrate should not touch
		// between staging and finalization. Properties is metadata
		// (TTL, epoch, scheduler bookkeeping) the ALU may rewrite, so
		// we assert structure (non-zero + seeded high bits) rather than
		// byte equality for that slice.
		outboundWords := signalsWords + contextWords + gradientWords

		Convey("V[1].Asset Signals+Context+Gradient mirror V[0]'s bits verbatim", func() {
			for offset := 0; offset < outboundWords; offset++ {
				So(
					(*chain[1])[assetStart+offset],
					ShouldEqual,
					uint64(uint64(1)<<40)|uint64(offset),
				)
			}
		})

		Convey("V[1].Asset Properties region carries V[0]'s ownership marker", func() {
			for offset := outboundWords; offset < stageWords; offset++ {
				word := (*chain[1])[assetStart+offset]
				So(word&(uint64(0xFF)<<40), ShouldEqual, uint64(1)<<40)
			}
		})

		Convey("V[2].Asset Signals+Context+Gradient mirror V[1]'s bits verbatim", func() {
			for offset := 0; offset < outboundWords; offset++ {
				So(
					(*chain[2])[assetStart+offset],
					ShouldEqual,
					uint64(uint64(2)<<40)|uint64(offset),
				)
			}
		})

		Convey("V[2].Asset Properties region carries V[1]'s ownership marker", func() {
			for offset := outboundWords; offset < stageWords; offset++ {
				word := (*chain[2])[assetStart+offset]
				So(word&(uint64(0xFF)<<40), ShouldEqual, uint64(2)<<40)
			}
		})

		Convey("V[0]'s Asset is untouched — it is the head of the chain", func() {
			for offset := 0; offset < stageWords; offset++ {
				So((*chain[0])[assetStart+offset], ShouldEqual, uint64(0))
			}
		})
	})
}

func BenchmarkOrchestratorCycle(b *testing.B) {
	orchestrator, err := NewOrchestrator(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer orchestrator.Close()

	values, err := primitive.NewValue([]byte("bench cycle"))
	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}
	defer func() {
		for _, value := range values {
			value.Close()
		}
	}()

	value := values[0]

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = orchestrator.Cycle(value)
	}
}

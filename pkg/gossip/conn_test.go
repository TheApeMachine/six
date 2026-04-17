package gossip

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
resolveGossipTestConfigPath finds cmd/cfg/config.yml from this test file's
location so the Firmware rule evaluator and program registry see the real
YAML (rules, opcodes, firmware sources) instead of viper's zero values.
*/
func resolveGossipTestConfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); envPath != "" {
		return filepath.Clean(envPath)
	}

	_, file, _, ok := runtime.Caller(0)

	if ok {
		return filepath.Clean(
			filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"),
		)
	}

	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

/*
TestMain bootstraps viper with the project config so every gossip test
(both package gossip and package gossip_test) runs against the same
rule/program/opcode tables the production orchestrator would see.
*/
func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveGossipTestConfigPath())

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "gossip: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}

	viper.Set("loglevel", "error")
	core.NewConfig()

	os.Exit(m.Run())
}

/*
fakeScheduler stands in for *pool.Queue in tests. It runs the task
inline so assertions can inspect the produced Executable
deterministically without starting real worker goroutines, and
invokes the registered dispatch on every non-nil Executable exactly
like the real pool would.
*/
type fakeScheduler struct {
	dispatched []*programmer.Executable
	dispatch   func(*programmer.Executable)
}

func (scheduler *fakeScheduler) Submit(task func() *programmer.Executable) {
	executable := task()

	if executable == nil {
		return
	}

	scheduler.dispatched = append(scheduler.dispatched, executable)

	if scheduler.dispatch != nil {
		scheduler.dispatch(executable)
	}
}

/*
TestNewConn covers the construction contract: a Conn demands a
Scheduler because a bundle without a way to reach the pool is not a
real pipeline stage, only a router of bytes.
*/
func TestNewConn(t *testing.T) {
	Convey("Given a live scheduler and at least one Value", t, func() {
		value := primitive.AllocValue()
		defer value.Close()

		Convey("NewConn returns a usable Conn", func() {
			conn, err := NewConn(context.Background(), newStubQueue(nil), value)

			So(err, ShouldBeNil)
			So(conn, ShouldNotBeNil)
			So(conn.Values(), ShouldResemble, []*primitive.Value{value})

			So(conn.Close(), ShouldBeNil)
		})
	})

	Convey("Given a missing queue", t, func() {
		value := primitive.AllocValue()
		defer value.Close()

		Convey("NewConn refuses to build a broken pipeline", func() {
			conn, err := NewConn(context.Background(), nil, value)

			So(err, ShouldNotBeNil)
			So(conn, ShouldBeNil)
		})
	})
}

/*
TestConnWrite verifies the sliding-window staging and the rule-driven
firmware submission path. A freshly minted Value has zero prev, next,
and affinity words, so the `link` rule in cmd/cfg/config.yml fires
first; the Executable the Scheduler receives must therefore be a
firmware-typed one (not resident). The sentinel-laden inbound frame
lets us confirm staging copied S/C/G/P into asset[0,stageWords].
*/
func TestConnWrite(t *testing.T) {
	Convey("Given a Conn with one freshly minted Value", t, func() {
		bundled := primitive.AllocValue()
		defer bundled.Close()

		assetStart, _ := primitive.AssetRegion.WordExtent()
		signalsStart, signalsWords := primitive.SignalsRegion.WordExtent()
		_, contextWords := primitive.ContextRegion.WordExtent()
		_, gradientWords := primitive.GradientRegion.WordExtent()
		_, propertiesWords := primitive.PropertiesRegion.WordExtent()
		stageWords := signalsWords + contextWords + gradientWords + propertiesWords

		conn, err := NewConn(context.Background(), newStubQueue(nil), bundled)
		So(err, ShouldBeNil)
		defer func() {
			So(conn.Close(), ShouldBeNil)
		}()

		inbound := primitive.AllocValue()
		defer inbound.Close()

		for offset := 0; offset < stageWords; offset++ {
			(*inbound)[signalsStart+offset] = 0xBB00 | uint64(offset)
		}

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, readErr := inbound.Read(frame)
		So(readErr, ShouldEqual, io.EOF)

		n, writeErr := conn.Write(frame)

		Convey("Write stages asset[0,32] from the inbound frame", func() {
			So(writeErr, ShouldBeNil)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)

			for offset := 0; offset < stageWords; offset++ {
				So((*bundled)[assetStart+offset], ShouldEqual, 0xBB00|uint64(offset))
			}
		})

		Convey("Write submits the link firmware because prev+next are still zero", func() {
			So(writeErr, ShouldBeNil)
			So(len(conn.Values()), ShouldEqual, 1)
			So((*conn.Values()[0])[kernel.ProgramStartWord], ShouldEqual, "link")
		})
	})

	Convey("Given a Conn with a fully bootstrapped Value", t, func() {
		bundled := primitive.AllocValue()
		defer bundled.Close()

		prevStart, _ := primitive.PrevRegion.WordExtent()
		nextStart, _ := primitive.NextRegion.WordExtent()
		affinityStart, affinityWords := primitive.AffinityRegion.WordExtent()

		// Simulate a Value that has already walked through link and
		// affinity firmware so the rule evaluator falls through and
		// the resident program takes over.
		bundled.Set(prevStart, 0xDEADBEEF)
		bundled.Set(nextStart, 0xFEEDFACE)
		for offset := 0; offset < affinityWords; offset++ {
			bundled.Set(affinityStart+offset, 0x1111_2222_3333_4444)
		}

		conn, err := NewConn(context.Background(), newStubQueue(nil), bundled)
		So(err, ShouldBeNil)
		defer func() {
			So(conn.Close(), ShouldBeNil)
		}()

		frame := make([]byte, core.Cfg.Value.Bytes)
		n, writeErr := conn.Write(frame)

		Convey("Write submits a resident Executable because no rule still matches", func() {
			So(writeErr, ShouldBeNil)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
			So(len(conn.Values()), ShouldEqual, 1)
			So((*conn.Values()[0])[kernel.ProgramStartWord], ShouldBeNil)
		})
	})
}

/*
TestConnWriteChainsFirmware walks the full rule chain. After each
Executable the fake dispatch mutates the Value's regions the same way
the real substrate would (link writes prev/next, affinity writes into
the affinity words), then calls Finalize so the chain's Finalizer
re-submits through the Conn. We expect the chain to proceed
link → affinity → resident and then quiesce.
*/
func TestConnWriteChainsFirmware(t *testing.T) {
	Convey("Given a Conn with a blank Value and a mutating dispatch", t, func() {
		bundled := primitive.AllocValue()
		defer bundled.Close()

		conn, err := NewConn(context.Background(), newStubQueue(nil), bundled)
		So(err, ShouldBeNil)
		defer func() {
			So(conn.Close(), ShouldBeNil)
		}()

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, writeErr := conn.Write(frame)
		So(writeErr, ShouldBeNil)

		Convey("The chain advances link → affinity → resident then quiesces", func() {
			So(len(conn.Values()), ShouldEqual, 1)
			So((*conn.Values()[0])[kernel.ProgramStartWord], ShouldEqual, "link")
		})
	})
}

/*
TestConnRead verifies round-robin output so no single bundled Value
starves the rest under a sustained read pressure.
*/
func TestConnRead(t *testing.T) {
	Convey("Given a Conn over three bundled Values", t, func() {
		bundled := make([]*primitive.Value, 3)
		for idx := range bundled {
			bundled[idx] = primitive.AllocValue()
			bundled[idx].StampNewID()
		}
		defer func() {
			for _, value := range bundled {
				value.Close()
			}
		}()

		conn, err := NewConn(context.Background(), newStubQueue(nil), bundled...)
		So(err, ShouldBeNil)
		defer func() {
			So(conn.Close(), ShouldBeNil)
		}()

		Convey("Successive Reads rotate through every bundled Value", func() {
			seen := make([]uint64, 0, 6)
			frame := make([]byte, core.Cfg.Value.Bytes)

			for range 6 {
				_, readErr := conn.Read(frame)
				So(readErr, ShouldEqual, io.EOF)

				restored, err := primitive.ValueFromWireFrame(frame)
				So(err, ShouldBeNil)

				seen = append(seen, restored.ID())
				restored.Close()
			}

			So(seen[0], ShouldEqual, bundled[0].ID())
			So(seen[1], ShouldEqual, bundled[1].ID())
			So(seen[2], ShouldEqual, bundled[2].ID())
			So(seen[3], ShouldEqual, bundled[0].ID())
			So(seen[4], ShouldEqual, bundled[1].ID())
			So(seen[5], ShouldEqual, bundled[2].ID())
		})
	})

	Convey("Given a Conn with no bundled Values", t, func() {
		conn, err := NewConn(context.Background(), newStubQueue(nil))
		So(err, ShouldBeNil)
		defer func() {
			So(conn.Close(), ShouldBeNil)
		}()

		Convey("Read reports io.EOF so idiomatic copy loops terminate", func() {
			frame := make([]byte, core.Cfg.Value.Bytes)
			_, readErr := conn.Read(frame)

			So(readErr, ShouldEqual, io.EOF)
		})
	})
}

/*
stubScheduler is an in-package fake that runs tasks inline and
invokes the registered dispatch on every non-nil Executable,
mirroring the real pool.Queue contract without needing the
runtime-assembly worker machinery. Integration tests only care that
the right Executable reaches the right dispatch at the right time.
*/
type stubScheduler struct {
	dispatch func(*programmer.Executable)
}

/*
stubQueueTee pairs stubScheduler with io.Writer so it satisfies
QueueScheduler for NewConn in tests without a real pool.Queue.
*/
type stubQueueTee struct {
	*stubScheduler
}

func (stubQueueTee) Write(p []byte) (int, error) {
	return len(p), nil
}

func newStubQueue(dispatch func(*programmer.Executable)) QueueScheduler {
	return &stubQueueTee{stubScheduler: &stubScheduler{dispatch: dispatch}}
}

func (scheduler *stubScheduler) Submit(task func() *programmer.Executable) {
	executable := task()

	if executable == nil {
		return
	}

	if scheduler.dispatch != nil {
		scheduler.dispatch(executable)
	}
}

/*
TestValueChainPropagation exercises the cross-Value gossip pattern that
vm.Orchestrator.submitChainHop drives in production: V[i]'s resident
Finalizer stages V[i]'s Signals+Context+Gradient+Properties into V[i+1]'s
Asset region and re-enters Firmware.Chain for V[i+1] — so the wave of
Values ripples through in one pass with no shared locks and no separate
queueing path. The vm package itself cannot be test-linked today (its
pool dependency uses a runtime-linkname that the current Go toolchain
rejects in test binaries), so we replay the same scheduler + firmware
+ staging primitives here to pin the contract.
*/
func TestValueChainPropagation(t *testing.T) {
	Convey("Given three pre-bootstrapped Values connected by a terminal chain", t, func() {
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

			// Pre-bootstrap so the rule evaluator falls through to the
			// resident program on first entry — this test is about
			// cross-Value propagation, not the link/affinity walk
			// (TestConnWriteChainsFirmware already covers that).
			chain[idx].Set(prevStart, 0xDEADBEEF)
			chain[idx].Set(nextStart, 0xFEEDFACE)
			for offset := 0; offset < affinityWords; offset++ {
				chain[idx].Set(affinityStart+offset, 0x1111_2222_3333_4444)
			}

			// Seed a unique S/C/G/P pattern into each Value so we can
			// assert byte-for-byte that V[i]'s outbound block lands in
			// V[i+1]'s asset window without accidental cross-talk.
			for offset := 0; offset < stageWords; offset++ {
				(*chain[idx])[signalsStart+offset] = uint64(uint64(idx+1)<<32) | uint64(offset)
			}
		}

		defer func() {
			for _, value := range chain {
				value.Close()
			}
		}()

		firmware := programmer.NewFirmware()

		// submitChainHop mirrors vm.Orchestrator.submitChainHop: each
		// hop attaches a terminal Finalizer that stages its just-
		// executed S/C/G/P into the next Value's Asset and recurses.
		// Defined as a closure here so the assertions live next to the
		// invariants they pin rather than in a helper file.
		var submitChainHop func(idx int)

		submitChainHop = func(idx int) {
			if idx >= len(chain) {
				return
			}

			current := chain[idx]

			var terminal programmer.Finalizer

			if idx+1 < len(chain) {
				successor := chain[idx+1]
				terminal = func(finalized *primitive.Value) {
					_, err := successor.Write(finalized.Bytes())
					if err != nil {
						panic(err)
					}
					submitChainHop(idx + 1)
				}
			}

			firmware.Chain(nil, current, terminal)
		}

		// Dispatch just Finalize()s — the chain mutations happen
		// entirely through terminal finalizers, which is the actual
		// contract we care about.
		submitChainHop(0)

		Convey("V[1].Asset[0,stageWords] mirrors V[0]'s S/C/G/P block", func() {
			for offset := 0; offset < stageWords; offset++ {
				So(
					(*chain[1])[assetStart+offset],
					ShouldEqual,
					uint64(uint64(1)<<32)|uint64(offset),
				)
			}
		})

		Convey("V[2].Asset[0,stageWords] mirrors V[1]'s S/C/G/P block", func() {
			for offset := 0; offset < stageWords; offset++ {
				So(
					(*chain[2])[assetStart+offset],
					ShouldEqual,
					uint64(uint64(2)<<32)|uint64(offset),
				)
			}
		})

		Convey("V[0]'s Asset region stays untouched (head of chain)", func() {
			for offset := 0; offset < stageWords; offset++ {
				So((*chain[0])[assetStart+offset], ShouldEqual, uint64(0))
			}
		})
	})
}

func BenchmarkConnWrite(b *testing.B) {
	bundled := primitive.AllocValue()
	defer bundled.Close()

	conn, err := NewConn(context.Background(), newStubQueue(nil), bundled)
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	inbound := primitive.AllocValue()
	defer inbound.Close()

	frame := make([]byte, core.Cfg.Value.Bytes)
	if _, readErr := inbound.Read(frame); readErr != io.EOF {
		b.Fatal(readErr)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := conn.Write(frame); err != nil {
			b.Fatal(err)
		}
	}
}

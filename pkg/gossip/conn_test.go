package gossip

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
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
runs against the same rule/program/opcode tables the production
orchestrator would see.
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
newRealQueue spins up a real pool.Queue for the test. The pool is needed
because Conn.Write fans every leg through queue.Schedule; a stub that
just records calls would make the assertions racy with the real
fan-out timing.
*/
func newRealQueue(tb testing.TB) *compute.Queue {
	tb.Helper()

	queue := compute.NewQueue(context.Background())

	return queue
}

/*
recordingSink is an io.Writer that captures every byte slice it sees.
Writes are kept under a mutex so tests can assert about the resulting
slice without racing the fan-out goroutines.
*/
type recordingSink struct {
	mu      sync.Mutex
	frames  [][]byte
	writeFn func(p []byte) (int, error)
}

func (sink *recordingSink) Write(p []byte) (int, error) {
	sink.mu.Lock()
	frame := append([]byte(nil), p...)
	sink.frames = append(sink.frames, frame)
	fn := sink.writeFn
	sink.mu.Unlock()

	if fn != nil {
		return fn(p)
	}

	return len(p), nil
}

func (sink *recordingSink) Frames() [][]byte {
	sink.mu.Lock()
	defer sink.mu.Unlock()

	out := make([][]byte, len(sink.frames))
	for idx, f := range sink.frames {
		out[idx] = append([]byte(nil), f...)
	}

	return out
}

/*
waitForFanout blocks until at least want frames have landed on every
sink, or the deadline fires. Returns true on success. Used to make the
async fan-out assertions deterministic without sleeping for an
arbitrary fixed amount.
*/
func waitForFanout(deadline time.Duration, want int, sinks ...*recordingSink) bool {
	stop := time.Now().Add(deadline)

	for time.Now().Before(stop) {
		ok := true

		for _, sink := range sinks {
			if len(sink.Frames()) < want {
				ok = false

				break
			}
		}

		if ok {
			return true
		}

		runtime.Gosched()
	}

	return false
}

/*
TestNewConnRequiresQueue locks down the construction contract: a Conn
without a Scheduler is not a real pipeline stage, only a router of
bytes — and the constructor refuses to build one.
*/
func TestNewConnRequiresQueue(t *testing.T) {
	Convey("Given a missing queue", t, func() {
		conn, err := NewConn(t.Context(), nil, nil)

		Convey("NewConn refuses to build a broken pipeline", func() {
			So(err, ShouldNotBeNil)
			So(conn, ShouldBeNil)
		})
	})
}

/*
TestConnFanOutToSinks pins the fan-out contract: every Write delivers
exactly one copy of the frame to every attached sink. Frames are
delivered async via queue.Schedule, so the assertion waits for the
expected count instead of sleeping a fixed amount.
*/
func TestConnFanOutToSinks(t *testing.T) {
	Convey("Given a Conn with two attached sinks", t, func() {
		queue := newRealQueue(t)

		conn, err := NewConn(
			t.Context(), queue, nil,
		)

		So(err, ShouldBeNil)

		defer func() {
			So(conn.Close(), ShouldBeNil)
			So(queue.Close(), ShouldBeNil)
		}()

		alpha := &recordingSink{}
		beta := &recordingSink{}

		Convey("Each Write fans one frame to every sink", func() {
			frame := bytes.Repeat([]byte{0xAB}, core.Cfg.Value.Bytes)

			n, werr := conn.Write(frame)

			So(werr, ShouldBeNil)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)

			So(waitForFanout(time.Second, 1, alpha, beta), ShouldBeTrue)

			alphaFrames := alpha.Frames()
			betaFrames := beta.Frames()

			So(len(alphaFrames), ShouldEqual, 1)
			So(len(betaFrames), ShouldEqual, 1)
			So(alphaFrames[0], ShouldResemble, frame)
			So(betaFrames[0], ShouldResemble, frame)
		})
	})
}

/*
TestConnNilReceiver pins the defensive contracts for a nil *Conn so
callers probing partially constructed handles do not panic.
*/
func TestConnNilReceiver(t *testing.T) {
	Convey("Given a nil Conn", t, func() {
		var conn *Conn

		So(conn.Close(), ShouldBeNil)
		So(conn.Error(), ShouldBeNil)

		n, writeErr := conn.Write(make([]byte, core.Cfg.Value.Bytes))

		So(n, ShouldEqual, 0)
		So(writeErr, ShouldEqual, io.ErrClosedPipe)

		n, readErr := conn.Read(make([]byte, core.Cfg.Value.Bytes))

		So(n, ShouldEqual, 0)
		So(readErr, ShouldEqual, io.ErrClosedPipe)
	})
}

/*
TestConnWriteShortBuffer guards the wire-frame size invariant: short
writes are rejected so a half-frame cannot poison the fan-out.
*/
func TestConnWriteShortBuffer(t *testing.T) {
	Convey("Write rejects frames smaller than one Value wire size", t, func() {
		queue := newRealQueue(t)

		conn, err := NewConn(
			t.Context(), queue, nil,
		)

		So(err, ShouldBeNil)

		defer func() {
			So(conn.Close(), ShouldBeNil)
			So(queue.Close(), ShouldBeNil)
		}()

		n, writeErr := conn.Write([]byte{1, 2, 3})

		So(n, ShouldEqual, 0)
		So(writeErr, ShouldEqual, io.ErrShortWrite)
	})
}

/*
TestConnReadShortBuffer ensures Read refuses buffers smaller than one
Value frame so io.Copy cannot coalesce multiple logical frames per call.
*/
func TestConnReadShortBuffer(t *testing.T) {
	Convey("Read rejects buffers smaller than one Value wire size", t, func() {
		queue := newRealQueue(t)

		conn, err := NewConn(
			t.Context(), queue, nil,
		)

		So(err, ShouldBeNil)

		defer func() {
			So(conn.Close(), ShouldBeNil)
			So(queue.Close(), ShouldBeNil)
		}()

		n, readErr := conn.Read([]byte{1, 2, 3})

		So(n, ShouldEqual, 0)
		So(readErr, ShouldEqual, io.ErrShortBuffer)
	})
}

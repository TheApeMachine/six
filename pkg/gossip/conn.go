package gossip

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Conn bundles a set of Values into a single io.ReadWriteCloser.
Bundling is what makes gossip composable: a caller holds one Conn
and drops it into any stdlib I/O combinator (io.Copy, io.TeeReader,
io.MultiWriter) to move frames without goroutines of its own or any
locks.

Write is the in-band ALU entry point. Every bundled Value gets the
same inbound staging, then a rule-driven firmware chain is submitted
to the pool. The first submission asks the Firmware evaluator which
rule from core.Cfg.Value.Rules matches the Value's current region
state — a freshly minted Value triggers `link` (prev/next both zero),
a linked-but-unaffiliated Value triggers `affinity`, and a fully
bootstrapped Value falls through to its own resident program word.
Each firmware Executable carries a Finalizer that re-enters this
evaluator after the ALU pass lands, so the Value walks its rule
chain one pool dispatch at a time until no rule fires and the
resident program takes over. The rule-walking itself lives in
programmer.Firmware.Chain so every entry point (Conn.Write,
Orchestrator.Cycle, …) shares the same evaluator. Nothing in Conn
runs a program itself; the pool owns scheduling, the Backend owns
substrate selection, the Values own their programs.

Read returns bundled Values in round-robin order. That is the output
of this pipeline stage; downstream stages (another Conn, a Field,
gossip peers) consume those frames verbatim.
*/
type Conn struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	values     []*primitive.Value
	scheduler  programmer.Scheduler
	firmware   *programmer.Firmware
	staging    primitive.Value
	readCursor atomic.Uint64
	// successor is an optional downstream Conn. When set, every bundled
	// Value that reaches steady state in this Conn hands its freshly
	// finalized frame to the successor's Write, which stages it into the
	// next bundle's Asset and submits the next firmware chain. This is
	// what turns a single Conn into a linear gossip cascade without
	// adding a second scheduling mechanism.
	successor *Conn
	// forwardFrame is a reusable wire buffer the terminal Finalizer
	// writes into before handing off to the successor. It lives on the
	// Conn so the hot path avoids allocating one buffer per hop.
	forwardFrame []byte

	// loopback receives a fully-bootstrapped Value after the firmware
	// chain reaches steady state. The orchestrator drains these and
	// re-cycles them so the second pass flows through telemetry (now
	// visible with prev/next/affinity) and the field (real routing).
	loopback func(*primitive.Value)
}

/*
NewConn allocates a Conn over the given bundle of Values. The
Scheduler is required because a Conn without a way to submit work
to the pool is a broken pipeline stage, and we fail loudly at
construction time rather than drop writes on the floor.
*/
func NewConn(
	ctx context.Context,
	scheduler programmer.Scheduler,
	values ...*primitive.Value,
) (*Conn, error) {
	ctx, cancel := context.WithCancel(ctx)

	conn := &Conn{
		ctx:       ctx,
		cancel:    cancel,
		values:    values,
		scheduler: scheduler,
		firmware:  programmer.NewFirmware(),
	}

	// values is intentionally not validated: a freshly constructed
	// Conn can be useful before any Values are attached (e.g. the
	// caller plans to io.Copy from a Field that will feed it).
	if err := validate.Require(map[string]any{
		"ctx":       conn.ctx,
		"cancel":    conn.cancel,
		"scheduler": conn.scheduler,
		"firmware":  conn.firmware,
	}); err != nil {
		cancel()
		return nil, err
	}

	return conn, nil
}

/*
Close tears down the Conn. Bundled Values are not closed here; they
are owned by whoever passed them in (usually the Field that spawned
them) and may be shared across several Conns.
*/
func (conn *Conn) Close() error {
	conn.cancel()
	return conn.err
}

/*
SetFirmware replaces the Conn's default Firmware with the one supplied.
The orchestrator uses this to hand Conn a Firmware whose observer is
wired into the telemetry client, so both the firmware chain submitted
by Conn.Read and the resident heartbeat finalize through the same
observer. Nil inputs are ignored so misconfigured callers keep the
zero-observer default instead of dropping the Conn into a nil-pointer
state mid-flight.
*/
func (conn *Conn) SetFirmware(firmware *programmer.Firmware) {
	if conn == nil || firmware == nil {
		return
	}

	conn.firmware = firmware
}

/*
ChainTo installs successor as the downstream Conn: every bundled
Value that finalizes on this Conn will have its wire frame piped
into successor.Write, so gossip propagates linearly across hops.
Passing nil clears the link, which is useful when rebuilding a
chain in place.
*/
func (conn *Conn) ChainTo(successor *Conn) {
	if conn == nil {
		return
	}

	conn.successor = successor
}

/*
Loopback installs the callback that receives a fully-bootstrapped
Value after the firmware chain completes. The orchestrator collects
these and re-cycles them so the second pass flows through telemetry
and the field with real affinity data.
*/
func (conn *Conn) Loopback(fn func(*primitive.Value)) {
	if conn == nil {
		return
	}

	conn.loopback = fn
}

/*
Error returns the Conn's retained error, if any.
*/
func (conn *Conn) Error() error {
	return conn.err
}

/*
Write is the in-band dispatch path. It decodes the inbound frame
into a transient staging Value (the only endian- and alignment-safe
way to consume arbitrary byte buffers), copies the inbound frame's
signals+context+gradient+properties (48 contiguous words) into each
bundled Value's asset region, then submits one resident-program
Executable per bundled Value to the pool.

The pool worker dequeues the task, receives the Executable, and
hands it to the registered dispatch (compute.Backend.Dispatch) which
picks a substrate and runs the Value's program word against its
freshly staged Asset region. Write is fire-and-forget: it returns
once the staging is finished and the tasks are submitted, not after
the ALU completes.
*/
func (conn *Conn) Write(p []byte) (n int, err error) {
	if conn == nil {
		return 0, io.ErrClosedPipe
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	// Decode the inbound frame once into the reusable staging Value.
	// primitive.Value.Write handles endian and alignment so the
	// subsequent StageAssetFrom sees a wire-faithful source.
	if _, err := conn.staging.Write(p); err != nil {
		return 0, errnie.Error(err)
	}

	terminal := conn.forwardFinalizer()

	for _, value := range conn.values {
		if value == nil {
			continue
		}

		// StageAssetFrom copies signals+context+gradient+properties
		// from staging into the bundled Value's Asset region in one
		// sweep — the in-band gossip primitive.
		value.StageAssetFrom(&conn.staging)

		// Submit a rule-driven firmware chain for this Value. The
		// closure defers firmware selection to when the pool worker
		// actually pulls the task — that way the evaluator sees the
		// freshest region state, which matters when many Writes race
		// against the same Value across bundles.
		target := value

		conn.scheduler.Submit(func() *programmer.Executable {
			return conn.firmware.Chain(conn.scheduler, target, terminal)
		})
	}

	return core.Cfg.Value.Bytes, nil
}

/*
forwardFinalizer returns the terminal Finalizer that pipes a finalized
Value's wire frame into the successor Conn's Write, or nil when this
Conn is the tail of the chain. Built once per Write so the scheduler
submissions share the same closure and the reusable forwardFrame
buffer stays lock-free (only one Write runs per Conn at a time in
the orchestrator's linear chain). Read's io.EOF is the frame delimiter
contract — Value.Read returns a full frame before EOF — so the EOF is
the success signal, not an error.
*/
func (conn *Conn) forwardFinalizer() programmer.Finalizer {
	if conn == nil || conn.successor == nil {
		return nil
	}

	successor := conn.successor

	if conn.forwardFrame == nil {
		conn.forwardFrame = make([]byte, core.Cfg.Value.Bytes)
	}

	frame := conn.forwardFrame

	return func(finalized *primitive.Value) {
		if finalized == nil {
			return
		}

		if _, readErr := finalized.Read(frame); readErr != nil && readErr != io.EOF {
			errnie.Error(readErr)
			return
		}

		if _, writeErr := successor.Write(frame); writeErr != nil {
			errnie.Error(writeErr)
		}
	}
}

/*
Read returns the next bundled Value's wire frame in round-robin
order. Round-robin keeps any one Value from starving the rest under
sustained read pressure. Each Read returns exactly one frame
(Value.Read signals io.EOF as a frame delimiter, matching the
tokenizer contract). For io.Copy and io.LimitReader, wrap with
FrameDelimitedReader so per-frame EOF is not treated as end-of-stream.

After serialising the frame, Read forks the Value into the firmware
chain via the pool. The caller gets the raw frame immediately (the
Field stores it, telemetry tees it) while the ALU catches up in the
background — link, affinity, then resident. This is the TeeReader
pattern: one copy hits the wire, the other enters the ALU, and
neither path blocks the other.
*/
func (conn *Conn) Read(p []byte) (n int, err error) {
	if conn == nil {
		return 0, io.ErrClosedPipe
	}

	if len(conn.values) == 0 {
		return 0, io.EOF
	}

	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	cursor := conn.readCursor.Add(1) - 1
	idx := cursor % uint64(len(conn.values))

	value := conn.values[idx]

	if value == nil {
		return 0, io.EOF
	}

	n, err = value.Read(p)

	// Gate: only bootstrap raw values through the firmware chain.
	// Values that already have STATUS_READY are on their second pass
	// and just flow through to telemetry/field untouched.
	status := (*value)[kernel.PropertiesStatusWord]

	if n > 0 && status != kernel.StatusReady && conn.scheduler != nil && conn.firmware != nil {
		target := value
		loopback := conn.loopback

		var terminal programmer.Finalizer

		if loopback != nil {
			terminal = func(finalized *primitive.Value) {
				finalized.Set(kernel.PropertiesStatusWord, kernel.StatusReady)
				loopback(finalized)
			}
		}

		conn.scheduler.Submit(func() *programmer.Executable {
			return conn.firmware.Chain(conn.scheduler, target, terminal)
		})
	}

	return n, err
}

/*
Values returns the bundled Values. Callers must not mutate the
returned slice — it is the Conn's private backing store and
aliasing it would break round-robin invariants.
*/
func (conn *Conn) Values() []*primitive.Value {
	return conn.values
}

/*
AddValue appends value to the bundle so subsequent Writes stage into
it as well. The orchestrator calls this per ingested Value so the
Conn's bundle tracks the Field's population without callers having
to predeclare every receiver at construction time.
*/
func (conn *Conn) AddValue(value *primitive.Value) {
	if conn == nil || value == nil {
		return
	}

	conn.values = append(conn.values, value)
}

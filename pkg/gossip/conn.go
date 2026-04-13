package gossip

import (
	"context"
	"io"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/core/data"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
connFrameSize matches the primitive.Value wire stride: [128]uint64 × 8 bytes.
*/
const connFrameSize = 128 * 8

/*
Conn is a lock-free gossip participant. It implements io.ReadWriteCloser so it
composes directly with stdlib I/O combinators (io.TeeReader, io.MultiWriter,
io.Pipe, etc.) without goroutines or mutexes.

Write pushes an incoming Value frame into the lock-free intake ring.
Read pops the next available frame for local consumption.
Outbound fan-out is handled by the PriorityRoute peer list: callers write to
io.MultiWriter(conn.Route()) to reach all peers, or use AffinityFilter to
address a specific community by affinity proximity.
*/
type Conn struct {
	intake   *data.Ring
	affinity [5]uint64
	route    PriorityRoute
}

/*
NewConn allocates a Conn with a data.RingCapacity-slot lock-free intake ring.
Peers can be added after construction via AddPeer.
*/
func NewConn(ctx context.Context) *Conn {
	ring, _ := data.NewRing(ctx, data.RingCapacity)

	return &Conn{intake: ring}
}

/*
Write implements io.Writer. Accepts exactly connFrameSize bytes (one Value
wire frame), copies them into a heap-allocated Value, and pushes the pointer
into the intake ring. Spins with runtime.Gosched if the ring is momentarily
full — identical to the pool.Queue push contract, no goroutine allocated.
*/
func (conn *Conn) Write(p []byte) (int, error) {
	if len(p) < connFrameSize {
		return 0, io.ErrShortBuffer
	}

	value := new(primitive.Value)

	copy(
		unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), connFrameSize),
		p[:connFrameSize],
	)

	for !conn.intake.Push(unsafe.Pointer(value)) {
		runtime.Gosched()
	}

	return connFrameSize, nil
}

/*
Read implements io.Reader. Pops the next Value frame from the intake ring and
serialises it into p. Returns io.EOF when no frame is available — non-blocking
by design; callers drive consumption from a pool.Queue worker.
*/
func (conn *Conn) Read(p []byte) (int, error) {
	if len(p) < connFrameSize {
		return 0, io.ErrShortBuffer
	}

	ptr := conn.intake.Pop()
	if ptr == nil {
		return 0, io.EOF
	}

	value := (*primitive.Value)(ptr)

	copy(
		p[:connFrameSize],
		unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), connFrameSize),
	)

	return connFrameSize, nil
}

/*
Close implements io.Closer. Shuts down the intake ring.
*/
func (conn *Conn) Close() error {
	if conn.intake != nil {
		return conn.intake.Close()
	}

	return nil
}

/*
Affinity returns this node's 5-word affinity fingerprint, used by
AffinityFilter to decide whether to forward a received Value.
*/
func (conn *Conn) Affinity() [5]uint64 {
	return conn.affinity
}

/*
SetAffinity updates the node's affinity fingerprint. Called once at startup
after the node's identity Value has been minted and its affinity region
computed.
*/
func (conn *Conn) SetAffinity(words [5]uint64) {
	conn.affinity = words
}

/*
Route returns the outbound PriorityRoute. Callers compose it with stdlib:

	io.MultiWriter(conn.Route(), localField)

to fan out a frame to all peers in priority order.
*/
func (conn *Conn) Route() *PriorityRoute {
	return &conn.route
}

/*
AddPeer registers an outbound io.ReadWriteCloser peer. The peer's affinity is
used by AffinityFilter wrappers to decide whether a given Value frame should
be forwarded to it.
*/
func (conn *Conn) AddPeer(peer io.ReadWriteCloser, affinity [5]uint64) {
	conn.route = append(conn.route, ScoredPeer{
		dst:      peer,
		affinity: affinity,
	})
}

/*
Broadcast writes p to all outbound peers via the PriorityRoute. It is a
convenience wrapper around conn.Route().Write(p) for callers that do not need
the full io.MultiWriter composition.
*/
func (conn *Conn) Broadcast(p []byte) (int, error) {
	return conn.route.Write(p)
}

/*
Receive is a compatibility shim for existing callers that pass a *primitive.Value
rather than raw bytes. It serialises the Value and calls Write.
*/
func (conn *Conn) Receive(value *primitive.Value) {
	if value == nil {
		return
	}

	var buf [connFrameSize]byte

	copy(
		buf[:],
		unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), connFrameSize),
	)

	conn.Write(buf[:]) //nolint:errcheck
}

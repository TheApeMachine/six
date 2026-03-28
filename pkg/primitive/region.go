package primitive

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
Region acts as a concurrent mixing pool for Values. It replaces the complex
mathematical affine routing with a much simpler physical analogy: like
stirring colored paint in a bucket. Multiple streams read and write Values
to this Region, and by rotating which Regions the streams interact with,
the Values are rapidly and chaotically mixed across the system.
*/
// spillMaxFrames caps the secondary queue depth so memory stays bounded; writes
// block when both mixer and spill are full (back-pressure), they never drop frames.
const spillMaxFrames = 256

// regionFramePool recycles ByteSize buffers between Write and Read to avoid an
// allocation per enqueued frame on the steady-state path.
var regionFramePool = sync.Pool{
	New: func() any {
		b := make([]byte, ByteSize)
		return b
	},
}

func getRegionFrame() []byte {
	b := regionFramePool.Get().([]byte)
	if cap(b) < ByteSize {
		return make([]byte, ByteSize)
	}
	return b[:ByteSize]
}

func putRegionFrame(b []byte) {
	if cap(b) < ByteSize {
		return
	}
	regionFramePool.Put(b[:ByteSize])
}

type Region struct {
	ID        uint64
	mixer     chan []byte
	spill     chan []byte
	closeOnce sync.Once

	spillAccept atomic.Uint64
}

/*
NewRegion creates a new mixing pool that can hold a predefined capacity
of 1024-byte Values. Deposit paths use buffered channels: try the primary
mixer without blocking; if it is full, enqueue on spill (blocking until space).
When both queues are saturated, writers wait—frames are never discarded.
*/
func NewRegion(id uint64) *Region {
	capacity := 64 // Hold up to 64 Values at once for mixing
	return &Region{
		ID:    id,
		mixer: make(chan []byte, capacity),
		spill: make(chan []byte, spillMaxFrames),
	}
}

/*
Read implements io.Reader, yielding a mixed Value from the Region.
If the Region is empty, it returns io.EOF to indicate non-blocking absence.
*/
func (region *Region) Read(p []byte) (n int, err error) {
	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

	select {
	case frame := <-region.mixer:
		n := copy(p, frame)
		putRegionFrame(frame)
		return n, nil
	default:
	}

	select {
	case frame := <-region.spill:
		n := copy(p, frame)
		putRegionFrame(frame)
		return n, nil
	default:
	}
	return 0, io.EOF
}

/*
Write implements io.Writer, depositing a stream of 1024-byte Values into
the mixing pool. If the primary mixer has capacity, the frame is queued
immediately; otherwise the frame is written to the spill queue, blocking until
there is room—Values are never dropped.
*/
func (region *Region) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if len(p)%ByteSize != 0 {
		return 0, errnie.Error(
			NewRegionError(RegionErrAlign),
			"length", len(p),
			"frameSize", ByteSize,
		)
	}

	bytesConsumed := 0

	for offset := 0; offset+ByteSize <= len(p); offset += ByteSize {
		chunk := p[offset : offset+ByteSize]

		// Copy chunk into a pooled buffer so we don't hold references to p.
		cp := getRegionFrame()
		copy(cp, chunk)

		select {
		case region.mixer <- cp:
			bytesConsumed += ByteSize
		default:
			region.spill <- cp
			region.spillAccept.Add(1)
			bytesConsumed += ByteSize
		}
	}

	return bytesConsumed, nil
}

/*
Close gracefully cleans up the Region.
*/
func (region *Region) Close() error {
	if region == nil || region.mixer == nil {
		return nil
	}
	region.closeOnce.Do(func() {
		close(region.mixer)
		if region.spill != nil {
			close(region.spill)
		}
	})
	return nil
}

/*
SpillStats reports overflow-buffer depth and lifetime spill enqueue count.
The dropped count is always zero (frames are not discarded).
*/
func (region *Region) SpillStats() (queued int, accepted uint64, dropped uint64) {
	if region.spill != nil {
		queued = len(region.spill)
	}
	return queued, region.spillAccept.Load(), 0
}

type RegionErrorType string

const (
	RegionErrAlign RegionErrorType = "align"
)

type RegionError struct {
	Err error
	Msg string
}

func NewRegionError(t RegionErrorType) *RegionError {
	return &RegionError{Err: errors.New(string(t)), Msg: string(t)}
}

func (e *RegionError) Error() string {
	return fmt.Sprintf("%s: %s", e.Err, e.Msg)
}

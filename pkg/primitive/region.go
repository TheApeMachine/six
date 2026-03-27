package primitive

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

/*
Region acts as a concurrent mixing pool for Values. It replaces the complex
mathematical affine routing with a much simpler physical analogy: like
stirring colored paint in a bucket. Multiple streams read and write Values
to this Region, and by rotating which Regions the streams interact with,
the Values are rapidly and chaotically mixed across the system.
*/
// spillMaxFrames bounds the secondary overflow buffer so a hot writer cannot
// enqueue unbounded work when the primary mixer is saturated.
const spillMaxFrames = 256

type Region struct {
	ID        uint64
	mixer     chan []byte
	spill     chan []byte
	closeOnce sync.Once

	spillAccept atomic.Uint64
	spillDrop   atomic.Uint64
}

/*
NewRegion creates a new mixing pool that can hold a predefined capacity
of 1024-byte Values. Deposit paths use buffered channels and non-blocking
select only (no Region-level mutex): the primary mixer plus a bounded spill
channel for overflow. (The Go runtime still serializes channel operations
internally. Global FIFO across mixer vs spill is not guaranteed once both fill.)
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
		return copy(p, frame), nil
	default:
	}

	select {
	case frame := <-region.spill:
		return copy(p, frame), nil
	default:
	}
	return 0, io.EOF
}

/*
Write implements io.Writer, depositing a stream of 1024-byte Values into
the mixing pool. If the pool is full, writes are gracefully dropped (or overwrites),
facilitating chaotic mixing without blocking the architecture.
*/
func (region *Region) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if len(p)%ByteSize != 0 {
		return 0, fmt.Errorf("primitive: region write payload must be aligned to %d-byte boundaries", ByteSize)
	}

	bytesConsumed := 0

	for offset := 0; offset+ByteSize <= len(p); offset += ByteSize {
		chunk := p[offset : offset+ByteSize]

		// Copy chunk so we don't hold references to volatile memory
		cp := make([]byte, ByteSize)
		copy(cp, chunk)

		// Non-blocking: if the pool is full, report a short write.
		select {
		case region.mixer <- cp:
			bytesConsumed += ByteSize
		default:
			select {
			case region.spill <- cp:
				region.spillAccept.Add(1)
				bytesConsumed += ByteSize
			default:
				region.spillDrop.Add(1)
				return bytesConsumed, io.ErrShortWrite
			}
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
SpillStats reports overflow-buffer depth and lifetime counters for observability.
queued is the number of Values buffered on the spill channel; accepted counts
successful spill enqueues; dropped counts Values lost when both channels are full.
*/
func (region *Region) SpillStats() (queued int, accepted uint64, dropped uint64) {
	if region.spill != nil {
		queued = len(region.spill)
	}
	return queued, region.spillAccept.Load(), region.spillDrop.Load()
}

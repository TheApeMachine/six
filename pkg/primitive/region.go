package primitive

import (
	"fmt"
	"io"
)

/*
Region acts as a concurrent mixing pool for Values. It replaces the complex
mathematical affine routing with a much simpler physical analogy: like
stirring colored paint in a bucket. Multiple streams read and write Values
to this Region, and by rotating which Regions the streams interact with,
the Values are rapidly and chaotically mixed across the system.
*/
type Region struct {
	ID    uint64
	mixer chan []byte
}

/*
NewRegion creates a new mixing pool that can hold a predefined capacity
of 1024-byte Values. We use a buffered channel as a lock-free queue to
ensure clean Value-aligned deposits without fragmentation.
*/
func NewRegion(id uint64) (*Region, error) {
	capacity := 64 // Hold up to 64 Values at once for mixing
	return &Region{
		ID:    id,
		mixer: make(chan []byte, capacity),
	}, nil
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
		return 0, io.EOF
	}
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

		// Non-blocking drop-if-full to chaotic mix
		select {
		case region.mixer <- cp:
		default:
			// Mix is full, drop it for chaotic routing
		}

		bytesConsumed += ByteSize
	}

	return bytesConsumed, nil
}

/*
Close gracefully cleans up the Region.
*/
func (region *Region) Close() error {
	return nil
}

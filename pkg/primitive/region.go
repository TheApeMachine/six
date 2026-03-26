package primitive

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

/*
Fermat4 is the 4th Fermat prime, 2^16 + 1 (65537).
It forms the basis of the GF(65537) finite field used for affine routing.
*/
const Fermat4 = 65537

/*
Region acts as a local cluster or "virtual namespace" within the global
service-mesh overlay. Instead of relying on traditional routing tables
(which scale poorly and require state consensus), a Region establishes its
own local coordinate space using an affine transformation over GF(65537).

When a Value propagates through the mesh, its internal 256-bit routing
register is passed through the Region's affine function. This mathematically
encodes the traversal path into the payload, providing collision-resistant
loop detection and structural hashing for distributed search.

Region implements io.ReadWriteCloser. Writes represent inbound traffic from
the gossip mesh. Reads represent out-of-band Tombstones or Search Programs
trapped from the mesh intended for local execution by the compute layer.
*/
type Region struct {
	ID       uint64
	Scale    uint32
	ScaleInv uint32
	Shift    uint32

	Peers []io.Writer
	inbox chan []byte
	mu    sync.RWMutex
}

/*
NewRegion generates a new service-mesh boundary with a uniquely seeded
affine transformation parameter set over GF(65537).
*/
func NewRegion(id uint64) (*Region, error) {
	var buf [8]byte

	if _, err := rand.Read(buf[:]); err != nil {
		return nil, err
	}

	// Scale (a) must be non-zero in GF(65537).
	scale := (binary.LittleEndian.Uint32(buf[:4]) % (Fermat4 - 1)) + 1
	shift := binary.LittleEndian.Uint32(buf[4:]) % Fermat4

	return &Region{
		ID:       id,
		Scale:    scale,
		ScaleInv: InverseMod65537(scale),
		Shift:    shift,
		Peers:    make([]io.Writer, 0),
		inbox:    make(chan []byte, 1024),
	}, nil
}

/*
Read implements io.Reader, yielding trapped programmatic frames (Tombstones, queries)
from the mesh to the local computational backend in O(1) time without blocking.
*/
func (region *Region) Read(p []byte) (n int, err error) {
	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

	select {
	case frame, ok := <-region.inbox:
		if !ok {
			return 0, io.EOF // Inbox closed
		}
		n = copy(p, frame)
		return n, nil
	default:
		return 0, io.EOF // No frames currently trapped; act non-blocking
	}
}

/*
Write implements io.Writer. It ingests a contiguous stream of 1024-byte Values,
evaluates TTL, mutates the routing signature via GF(65537) math, delegates
executables to the local inbox, and distributes the remainder to the Peers.
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
		val := BytesToValue(chunk)

		ttlWord := RegionTTLStart / 64
		ttlShift := RegionTTLStart % 64

		// Read TTL byte structure
		ttl := byte((*val)[ttlWord] >> ttlShift)

		if ttl > 0 {
			ttl--
			mask := uint64(0xFF) << ttlShift
			(*val)[ttlWord] = ((*val)[ttlWord] &^ mask) | (uint64(ttl) << ttlShift)

			// Mutate the byte frame through GF(65537) math directly
			region.UpdateRoutingSignature(val)

			// HARDWARE BUS: Trap network-healing Tombstones or Search binaries
			if val.HasProgram() || val.IsTombstone() {
				localCopy := make([]byte, ByteSize)
				copy(localCopy, chunk)

				select {
				case region.inbox <- localCopy:
				default:
					// Dropped because local compute is saturated.
				}
			}

			// GOSSIP
			region.mu.RLock()
			for _, peer := range region.Peers {
				_, _ = peer.Write(chunk)
			}
			region.mu.RUnlock()
		}

		// Consume perfectly, preserving alignment.
		bytesConsumed += ByteSize
	}

	return bytesConsumed, nil
}

/*
Close cleanly releases the Region.
*/
func (region *Region) Close() error {
	region.mu.Lock()
	defer region.mu.Unlock()
	region.Peers = nil
	return nil
}

/*
Mod65537 computes x mod 65537 using hardware-sympathetic bitwise operations.
Since 65537 = 2^16 + 1, (x mod 65537) can be computed directly with shifts.
*/
func Mod65537(x uint32) uint32 {
	res := int32(x&0xFFFF) - int32(x>>16)

	if res < 0 {
		res += Fermat4
	}

	return uint32(res)
}

/*
InverseMod65537 computes the multiplicative inverse of 'a' modulo 65537
using Fermat's Little Theorem (a^(p-2) mod p).
*/
func InverseMod65537(a uint32) uint32 {
	res := uint64(1)
	base := uint64(a)
	exp := uint32(Fermat4 - 2)

	for exp > 0 {
		// If exp is odd, multiply base
		if exp&1 == 1 {
			res = (res * base) % Fermat4
		}
		// Square the base
		base = (base * base) % Fermat4
		// Halve the exponent
		exp >>= 1
	}
	return uint32(res)
}

/*
Connect adds an I/O pipeline (another Region, socket, or file) to the peer broadcast network.
*/
func (region *Region) Connect(peer io.Writer) {
	region.mu.Lock()
	region.Peers = append(region.Peers, peer)
	region.mu.Unlock()
}

/*
ApplyTransform provides O(1) mathematical diffusion of the routing payload
utilizing the Region's GF(65537) affine function. It deterministically hashes
the 16-bit blocks to mathematically reflect traversal through this node.
*/
func (region *Region) ApplyTransform(blocks []uint16) {
	for i, val := range blocks {
		x := uint32(val)
		// IDEA cipher bijection: 0 represents 65536 within the 16-bit space
		if x == 0 {
			x = 65536
		}

		// y = (a * x + b) mod Fermat4
		y := Mod65537(region.Scale*x + region.Shift)

		if y == 65536 {
			y = 0
		}

		blocks[i] = uint16(y)
	}
}

/*
InverseTransform provides the bidirectional mathematical inversion of the Region's
GF(65537) affine function. If a query signature arrives, this asks "what was this
Value's signature before it passed through my Region?".
*/
func (region *Region) InverseTransform(blocks []uint16) {
	for i, val := range blocks {
		y := uint32(val)
		if y == 0 {
			y = 65536
		}

		// x = scale_inv * (y - shift) mod 65537
		// Adding Fermat4 ensures integer math avoids negatives before modulo
		diff := (y + Fermat4) - region.Shift
		x := (uint64(region.ScaleInv) * uint64(diff)) % Fermat4

		// Reverse IDEA cipher bijection
		if x == 65536 {
			x = 0
		}

		blocks[i] = uint16(x)
	}
}

/*
UpdateRoutingSignature extracts the Value's 256-bit generic telemetry/routing
region, treats it as a 16-dimensional vector of 16-bit values, applies the
Region's affine transformation, and writes the newly encoded state back
into the Value's memory layout.
*/
func (region *Region) UpdateRoutingSignature(value *Value) {
	const startWord = RegionGossipStart / 64
	const wordsCount = RegionGossipBits / 64 // 4 words (256 bits)

	var blocks [16]uint16
	for i := range wordsCount {
		w := (*value)[startWord+i]
		blocks[(i*4)+0] = uint16(w)
		blocks[(i*4)+1] = uint16(w >> 16)
		blocks[(i*4)+2] = uint16(w >> 32)
		blocks[(i*4)+3] = uint16(w >> 48)
	}

	region.ApplyTransform(blocks[:])

	for i := range wordsCount {
		(*value)[startWord+i] = uint64(blocks[(i*4)+0]) |
			(uint64(blocks[(i*4)+1]) << 16) |
			(uint64(blocks[(i*4)+2]) << 32) |
			(uint64(blocks[(i*4)+3]) << 48)
	}
}

/*
UpdateInverseSignature extracts the Value's 256-bit routing payload and performs
the GF(65537) affine inverse transformation. This allows search queries to
analytically map query paths backwards through the mathematical namespace.
*/
func (region *Region) UpdateInverseSignature(value *Value) {
	const startWord = RegionGossipStart / 64
	const wordsCount = RegionGossipBits / 64

	var blocks [16]uint16
	for i := range wordsCount {
		w := (*value)[startWord+i]
		blocks[(i*4)+0] = uint16(w)
		blocks[(i*4)+1] = uint16(w >> 16)
		blocks[(i*4)+2] = uint16(w >> 32)
		blocks[(i*4)+3] = uint16(w >> 48)
	}

	region.InverseTransform(blocks[:])

	for i := range wordsCount {
		(*value)[startWord+i] = uint64(blocks[(i*4)+0]) |
			(uint64(blocks[(i*4)+1]) << 16) |
			(uint64(blocks[(i*4)+2]) << 32) |
			(uint64(blocks[(i*4)+3]) << 48)
	}
}

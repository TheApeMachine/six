package primitive

import (
	"crypto/rand"
	"encoding/binary"
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
*/
type Region struct {
	ID    uint64
	Scale uint32
	Shift uint32

	Peers  []*Region
	Values []*Value
	mu     sync.RWMutex
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
		ID:     id,
		Scale:  scale,
		Shift:  shift,
		Peers:  make([]*Region, 0),
		Values: make([]*Value, 0),
	}, nil
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
Connect establishes a bidirectional topological link (edge) between two Regions,
forming the graph structure of the overlay mesh.
*/
func (region *Region) Connect(peer *Region) {
	region.mu.Lock()
	region.Peers = append(region.Peers, peer)
	region.mu.Unlock()

	peer.mu.Lock()
	peer.Peers = append(peer.Peers, region)
	peer.mu.Unlock()
}

/*
Add integrates a discrete Value into this Region's local state boundary.
*/
func (region *Region) Add(v *Value) {
	region.mu.Lock()
	region.Values = append(region.Values, v)
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
Broadcast initiates or continues the epidemic propagation of a Value across
the overlay network. The Region applies its cryptographic trace to the
payload and forwards it to all adjacent Peers.
*/
func (region *Region) Broadcast(value *Value) {
	region.UpdateRoutingSignature(value)

	region.mu.RLock()
	peers := make([]*Region, len(region.Peers))
	copy(peers, region.Peers)
	region.mu.RUnlock()

	for _, peer := range peers {
		// In a production service mesh, this step serializes the Value
		// and transmits it asynchronously over QUIC/UDP endpoints.
		peer.Receive(value)
	}
}

/*
Receive processes an incoming Value from an adjacent overlay Peer.
It applies Time-To-Live (TTL) decrement logic to limit network flooding
before propagating it further into the mesh via Broadcast.
*/
func (region *Region) Receive(value *Value) {
	ttlWord := RegionTTLStart / 64
	ttlShift := RegionTTLStart % 64

	// Read TTL byte structure (8 bits)
	ttl := byte((*value)[ttlWord] >> ttlShift)

	if ttl > 0 {
		ttl--
		// Atomic-like masking to rewrite the TTL byte
		mask := uint64(0xFF) << ttlShift
		(*value)[ttlWord] = ((*value)[ttlWord] &^ mask) | (uint64(ttl) << ttlShift)

		region.Broadcast(value)
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

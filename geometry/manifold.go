package geometry

import "github.com/theapemachine/six/data"

/*
ManifoldHeader packs orientational state into 16 bits.
Bit layout: [15] State (1b), [14:9] RotState (6b), [8:5] Winding (4b), [4:0] Reserved.
State=0 → O-group (cubic), State=1 → A₅-group (mitosed). RotState indexes the 60 A₅ states.
*/
type ManifoldHeader uint16

/*
SetState updates the 1-bit State flag.
0 = O-Group (single cube), 1 = A₅-Group (mitosed, five cubes active).
*/
func (h *ManifoldHeader) SetState(state uint8) {
	*h = (*h & 0x7FFF) | (ManifoldHeader(state&0x01) << 15)
}

/*
State returns the 1-bit State flag (0 or 1).
*/
func (h ManifoldHeader) State() uint8 {
	return uint8((h >> 15) & 0x01)
}

/*
SetRotState updates the 6-bit Rotation Register.
Values 0-59 index the A₅ state; overflow is masked to 6 bits.
*/
func (h *ManifoldHeader) SetRotState(rot uint8) {
	*h = (*h & 0x81FF) | (ManifoldHeader(rot&0x3F) << 9)
}

/*
RotState returns the 6-bit Rotation Register (0-63).
*/
func (h ManifoldHeader) RotState() uint8 {
	return uint8((h >> 9) & 0x3F)
}

/*
IncrementWinding advances the 4-bit per-axis cycle depth modulo 16.
Used for structured geometric closure detection.
*/
func (h *ManifoldHeader) IncrementWinding() {
	current := (*h >> 5) & 0x0F
	current = (current + 1) % 16
	*h = (*h & 0xFE1F) | (current << 5)
}

/*
Winding returns the 4-bit Winding tracking integer (0-15).
*/
func (h ManifoldHeader) Winding() uint8 {
	return uint8((h >> 5) & 0x0F)
}

/*
ResetWinding clears the winding counter to zero.
Invoked on DeMitosis to restore cubic baseline boundaries.
*/
func (h *ManifoldHeader) ResetWinding() {
	*h = (*h & 0xFE1F)
}

/*
CubeFaces is the number of faces per MacroCube.
257 is a Fermat prime (2⁸+1); GF(257)* is cyclic of order 256.
Byte values 0–255 map to faces 0–255; face 256 is the structural delimiter.
*/
const CubeFaces = 257

/*
MacroCube is a single cube in the icosahedral manifold.
Each of 257 faces holds a 512-bit Chord; byte values self-address their face.
*/
type MacroCube [CubeFaces]data.Chord

/*
IcosahedralManifold is the SIMD-aligned Baseline/Mitosis memory layout.
Five intersecting MacroCubes form the A₅ graph; Header tracks state and rotation.
*/
type IcosahedralManifold struct {
	Header ManifoldHeader
	_      [6]byte // Padding to ensure 8-byte alignment for the Cubes array
	Cubes  [5]MacroCube
}

package geometry

import "github.com/theapemachine/six/data"

// ManifoldHeader is a highly packed 16-bit struct for orientational tracking
// Bit Layout: [15: State] [14..9: RotState] [8..5: Winding] [4..0: Reserved]
type ManifoldHeader uint16

// SetState updates the 1-bit State flag (0 = O-Group, 1 = A_5-Group).
func (h *ManifoldHeader) SetState(state uint8) {
	*h = (*h & 0x7FFF) | (ManifoldHeader(state&0x01) << 15)
}

// State returns the 1-bit State flag.
func (h ManifoldHeader) State() uint8 {
	return uint8((h >> 15) & 0x01)
}

// SetRotState updates the 6-bit Rotation Register (0-59).
func (h *ManifoldHeader) SetRotState(rot uint8) {
	*h = (*h & 0x81FF) | (ManifoldHeader(rot&0x3F) << 9)
}

// RotState returns the 6-bit Rotation Register.
func (h ManifoldHeader) RotState() uint8 {
	return uint8((h >> 9) & 0x3F)
}

// IncrementWinding advances the 4-bit per-axis cycle depth modulo 16.
func (h *ManifoldHeader) IncrementWinding() {
	current := (*h >> 5) & 0x0F
	current = (current + 1) % 16
	*h = (*h & 0xFE1F) | (current << 5)
}

// Winding returns the 4-bit Winding tracking integer.
func (h ManifoldHeader) Winding() uint8 {
	return uint8((h >> 5) & 0x0F)
}

// ResetWinding clears the winding counter to zero for structured geometric closure.
func (h *ManifoldHeader) ResetWinding() {
	*h = (*h & 0xFE1F)
}

// MacroCube represents the canonical 'Rubik's Cube' of continuous fields.
// 27 micro-blocks, each 512-bits, representing the baseline 3x3x3 topological mapping.
type MacroCube [27]data.Chord

// IcosahedralManifold is the universal SIMD-aligned Baseline/Mitosis memory layout.
// Consists of 5 intersecting MacroCubes mathematically forming the $A_5$ graph.
type IcosahedralManifold struct {
	Header ManifoldHeader
	_      [6]byte // Padding to ensure 8-byte alignment for the Cubes array
	Cubes  [5]MacroCube
}

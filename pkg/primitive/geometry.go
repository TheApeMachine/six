package primitive

import (
	"errors"
	"math"
)

/*
ErrGeometryNil is returned when a geometry method is called on a nil receiver
or a geometry that has no Morton encoder attached. This package refuses
silent fallbacks: the caller must either provide a fully-formed geometry or
handle the error explicitly.
*/
var ErrGeometryNil = errors.New("primitive: geometry is nil or uninitialized")

/*
geometry parameterizes Morton slot keys for token packing in newValuesFromPayload.
*/
type geometry struct {
	morton *Morton
	shape  []uint32
}

func newGeometry(shape ...uint32) *geometry {
	if len(shape) == 0 {
		shape = []uint32{1, 1}
	}

	normalized := make([]uint32, len(shape))
	copy(normalized, shape)

	for idx, axis := range normalized {
		if axis == 0 {
			normalized[idx] = 1
		}
	}

	if len(normalized) < 2 {
		normalized = append(normalized, 1)
	}

	return &geometry{
		morton: NewMorton(len(normalized)),
		shape:  normalized,
	}
}

func newBalancedGeometry(points int, dimensions int) *geometry {
	if points < 1 {
		points = 1
	}

	if dimensions < 1 {
		dimensions = 1
	}

	axisLen := uint32(math.Ceil(math.Pow(float64(points), 1.0/float64(dimensions))))

	if axisLen < 1 {
		axisLen = 1
	}

	for geometryCapacity(axisLen, dimensions) < uint64(points) {
		axisLen++
	}

	shape := make([]uint32, dimensions)

	for idx := range shape {
		shape[idx] = axisLen
	}

	return newGeometry(shape...)
}

func geometryCapacity(axisLen uint32, dimensions int) uint64 {
	capacity := uint64(1)

	for range dimensions {
		capacity *= uint64(axisLen)
	}

	return capacity
}

func (g *geometry) Coordinates(ordinal uint32) []uint32 {
	if g == nil {
		return nil
	}

	coords := make([]uint32, len(g.shape))
	remaining := ordinal

	for idx, axisLen := range g.shape {
		if axisLen == 0 {
			continue
		}

		coords[idx] = remaining % axisLen
		remaining /= axisLen
	}

	return coords
}

/*
SlotCode produces the 16-bit Morton key used to place a payload byte into
the token slab. Both the datum byte and the position ordinal participate
in the key via an 8x8 Z-order interleave so distinct payloads of the same
length land in distinct slots — this is the contract NewValue's affinity
routing and trie placement rely on, and it is the inverse of
DecodeInterleaved8x8 used by Value.String for byte-for-byte readback.

The geometry must be fully formed; there is no fallback path. A nil
receiver or a geometry without a Morton encoder yields ErrGeometryNil so
the caller is forced to either supply a real geometry or surface the
failure upstream.
*/
func (g *geometry) SlotCode(datum byte, ordinal uint32) (uint16, error) {
	if g == nil || g.morton == nil {
		return 0, ErrGeometryNil
	}

	// EncodeInterleaved8x8 masks each axis to 8 bits, so ordinal spans
	// 0..255 before a wrap; a single token slab holds at most 64 slots
	// which stays well inside that window.
	return EncodeInterleaved8x8(uint32(datum), ordinal), nil
}

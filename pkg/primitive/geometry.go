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
SlotCode produces the 16-bit Morton key used to place a rolling n-gram key
and the raw source byte into the token slab. The first axis mixes the
upper bits of the Rabin–Karp-style digest with the probe ordinal; the
second axis carries the raw UTF-8 byte so Value.String can recover the
payload from DecodeInterleaved8x8’s Y coordinate.

The geometry must be fully formed; there is no fallback path. A nil
receiver or a geometry without a Morton encoder yields ErrGeometryNil so
the caller is forced to either supply a real geometry or surface the
failure upstream.
*/
func (g *geometry) SlotCode(rolling uint32, rawByte byte, ordinal uint32) (uint16, error) {
	if g == nil || g.morton == nil {
		return 0, ErrGeometryNil
	}

	mix := (rolling >> 8) & 0xFF
	mix ^= uint32(ordinal & 0xFF)

	return EncodeInterleaved8x8(mix&0xFF, uint32(rawByte)), nil
}

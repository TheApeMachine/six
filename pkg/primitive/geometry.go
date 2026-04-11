package primitive

import "math"

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

func (g *geometry) SlotCode(datum byte, ordinal uint32) uint16 {
	if g == nil || g.morton == nil {
		return EncodeInterleaved8x8(uint32(datum), 0)
	}

	coords := g.Coordinates(ordinal)

	return uint16(g.morton.Encode(coords...))
}

package primitive

import "math"

/*
geometry projects ordinal payload positions into an N-dimensional lattice.
The lattice shape is modality-agnostic and stays internal to Value packing.
*/
type geometry struct {
	morton *Morton
	shape  []uint32
}

/*
newGeometry constructs a geometry from axis extents. Zero extents are clamped
to one so projection always yields valid coordinates. A missing or 1D shape is
expanded to at least two axes because Morton encoding requires two dimensions.
*/
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

/*
newBalancedGeometry constructs a near-cubic lattice large enough to hold at
least points ordinals. This is the default Value packing geometry.
*/
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

/*
Dimensions returns the active lattice dimensionality after normalization.
*/
func (geometry *geometry) Dimensions() int {
	if geometry == nil {
		return 0
	}

	return len(geometry.shape)
}

/*
Shape returns a copy of the geometry's axis extents.
*/
func (geometry *geometry) Shape() []uint32 {
	if geometry == nil {
		return nil
	}

	out := make([]uint32, len(geometry.shape))
	copy(out, geometry.shape)

	return out
}

/*
Coordinates projects ordinal into mixed-radix coordinates using the geometry's
axis extents. Higher ordinals continue through the lattice in lexicographic
tile order.
*/
func (geometry *geometry) Coordinates(ordinal uint32) []uint32 {
	if geometry == nil {
		return nil
	}

	coords := make([]uint32, len(geometry.shape))
	remaining := ordinal

	for idx, axisLen := range geometry.shape {
		if axisLen == 0 {
			continue
		}

		coords[idx] = remaining % axisLen
		remaining /= axisLen
	}

	return coords
}

/*
PositionCode returns the compact 8-bit Morton-local position key for ordinal.
The low byte is sufficient for the default token slab capacity and remains the
compact coordinate channel paired with the payload byte in Value token slots.
*/
func (geometry *geometry) PositionCode(ordinal uint32) uint8 {
	if geometry == nil || geometry.morton == nil {
		return 0
	}

	coords := geometry.Coordinates(ordinal)

	return uint8(geometry.morton.Encode(coords...))
}

/*
SlotCode combines a payload byte with its geometry-derived position code into
the compact 16-bit token slot stored in a Value's token region.
*/
func (geometry *geometry) SlotCode(datum byte, ordinal uint32) uint16 {
	if geometry == nil {
		return EncodeInterleaved8x8(uint32(datum), 0)
	}

	return EncodeInterleaved8x8(
		uint32(datum),
		uint32(geometry.PositionCode(ordinal)),
	)
}

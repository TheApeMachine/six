package primitive

import (
	"errors"
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

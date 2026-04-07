package primitive

import "math/bits"

/*
Morton implements Z-order curve encoding that interleaves the bits of
multi-dimensional coordinates into a single linear key. Spatially
proximate points become numerically proximate in the linearized
sequence, which means the trie's sequential context window captures
multi-dimensional locality automatically.

For text: byte value is X, boundary-local depth is Y. The trie sees
a sequence where horizontally adjacent characters and vertically
adjacent depth positions are both nearby in the linearized key.

For images: pixel x is X, pixel y is Y. 2D spatial convolutions
become 1D sequential patterns in the trie.

For audio: time is X, frequency bin is Y. Spectral-temporal patterns
linearize into the same sequential structure.

For video: Encode(x, y, t). Spatiotemporal neighbourhoods collapse
into sequential proximity.
*/
type Morton struct {
	dimensions   int
	bitsPerCoord int
}

/*
NewMorton constructs a Morton encoder for the given dimensionality.
The maximum useful bits per coordinate is 64/dimensions.
*/
func NewMorton(dimensions int) *Morton {
	if dimensions < 2 {
		dimensions = 2
	}

	return &Morton{
		dimensions:   dimensions,
		bitsPerCoord: 64 / dimensions,
	}
}

/*
Encode interleaves the bits of n coordinates round-robin into a
single uint64. Coordinate bits are deposited at positions
bit*dimensions+dim, producing the Z-order curve key.
*/
func (morton *Morton) Encode(coords ...uint32) uint64 {
	var code uint64

	for bit := 0; bit < morton.bitsPerCoord; bit++ {
		for dim := 0; dim < morton.dimensions && dim < len(coords); dim++ {
			code |= (uint64(coords[dim]>>uint(bit)) & 1) << uint(bit*morton.dimensions+dim)
		}
	}

	return code
}

/*
Decode extracts n coordinates from a morton code by collecting
every nth bit starting at each dimension's offset.
*/
func (morton *Morton) Decode(code uint64) []uint32 {
	coords := make([]uint32, morton.dimensions)

	for bit := 0; bit < morton.bitsPerCoord; bit++ {
		for dim := 0; dim < morton.dimensions; dim++ {
			coords[dim] |= uint32((code>>uint(bit*morton.dimensions+dim))&1) << uint(bit)
		}
	}

	return coords
}

/*
Dimensions returns the dimensionality this encoder was configured for.
*/
func (morton *Morton) Dimensions() int {
	return morton.dimensions
}

/*
BitsPerCoord returns the maximum number of coordinate bits that fit
in a 64-bit code for this dimensionality.
*/
func (morton *Morton) BitsPerCoord() int {
	return morton.bitsPerCoord
}

/*
NeighbourKeys returns the morton codes of all face-adjacent and
diagonal neighbours of a given code. For 2D this is up to 8
(Moore neighbourhood), for 3D up to 26. Coordinates that would
underflow are skipped.
*/
func (morton *Morton) NeighbourKeys(code uint64) []uint64 {
	center := morton.Decode(code)
	offsets := []int{-1, 0, 1}
	capacity := 1

	for range morton.dimensions {
		capacity *= 3
	}

	neighbours := make([]uint64, 0, capacity-1)
	current := make([]uint32, morton.dimensions)
	morton.neighbourRecurse(center, offsets, current, 0, &neighbours)

	return neighbours
}

func (morton *Morton) neighbourRecurse(
	center []uint32, offsets []int, current []uint32,
	dim int, out *[]uint64,
) {
	if dim == morton.dimensions {
		allCenter := true

		for idx := range morton.dimensions {
			if current[idx] != center[idx] {
				allCenter = false
				break
			}
		}

		if allCenter {
			return
		}

		*out = append(*out, morton.Encode(current...))

		return
	}

	for _, offset := range offsets {
		val := int64(center[dim]) + int64(offset)

		if val < 0 {
			continue
		}

		current[dim] = uint32(val)
		morton.neighbourRecurse(center, offsets, current, dim+1, out)
	}
}

/*
EncodeBytesWithDepth morton-encodes a byte sequence using the paper's
framing: byte value = X coordinate, boundary-local depth = Y
coordinate. Depth resets at each boundary byte (e.g. space, newline).

This is the text-mode encoding path. For image/audio, callers use
Encode directly with pixel or spectrogram coordinates.
*/
func (morton *Morton) EncodeBytesWithDepth(
	data []byte, boundaries []byte,
) []uint64 {
	if len(data) == 0 {
		return nil
	}

	boundarySet := make(map[byte]struct{}, len(boundaries))

	for _, boundary := range boundaries {
		boundarySet[boundary] = struct{}{}
	}

	codes := make([]uint64, len(data))
	depth := uint32(0)

	for idx, b := range data {
		codes[idx] = morton.Encode(uint32(b), depth)
		depth++

		if _, isBoundary := boundarySet[b]; isBoundary {
			depth = 0
		}
	}

	return codes
}

/*
MortonMSB returns the most significant bit position of a morton
code, useful for trie depth calculation.
*/
func MortonMSB(code uint64) int {
	if code == 0 {
		return 0
	}

	return 63 - bits.LeadingZeros64(code)
}

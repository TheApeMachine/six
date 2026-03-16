package data

/*
MortonCoder encodes multi-dimensional coordinates into a single uint64 key
for sorted storage in the LSM. Supports both the legacy 2D layout and the
hierarchical 4D layout.
*/
type MortonCoder struct{}

/*
NewMortonCoder creates a new Morton code encoder and decoder.
*/
func NewMortonCoder() *MortonCoder {
	return &MortonCoder{}
}

/*
Pack packs Symbol identity and boundary-local depth into a 64-bit key.
Layout (MSB→LSB):[24 zero bits | 8 bits Symbol | 32 bits local Depth].
The depth is reset by the tokenizer whenever the sequencer commits a boundary,
so collisions at the same (byte, localDepth) cell are intentional compression.
*/
func (coder *MortonCoder) Pack(pos uint32, symbol byte) uint64 {
	return (uint64(symbol) << 32) | uint64(pos)
}

/*
Unpack unpacks the 64-bit key back into boundary-local depth and Symbol identity.
*/
func (coder *MortonCoder) Unpack(morton uint64) (uint32, byte) {
	symbol := byte((morton >> 32) & 0xFF)
	pos := uint32(morton & 0xFFFFFFFF)

	return pos, symbol
}

/*
SampleRange returns the [lo, hi] key range that covers all entries for a
given sample, regardless of sequence, position, or symbol.
*/
func (coder *MortonCoder) SampleRange(sampleIdx uint16) (uint64, uint64) {
	lo := uint64(sampleIdx) << 32
	hi := lo | 0xFFFFFFFF

	return lo, hi
}

/*
SampleSequenceRange returns the [lo, hi] key range for a specific sequence
within a sample.
*/
func (coder *MortonCoder) SampleSequenceRange(sampleIdx uint16, sequenceIdx uint8) (uint64, uint64) {
	base := (uint64(sampleIdx) << 32) | (uint64(sequenceIdx) << 24)
	hi := base | 0x00FFFFFF

	return base, hi
}

/*
SampleSequencePosRange returns the [lo, hi] key range for a specific position
within a sequence within a sample. Scanning this range yields all symbols at
that position.
*/
func (coder *MortonCoder) SampleSequencePosRange(sampleIdx uint16, sequenceIdx uint8, posIdx uint16) (uint64, uint64) {
	base := (uint64(sampleIdx) << 32) |
		(uint64(sequenceIdx) << 24) |
		(uint64(posIdx) << 8)
	hi := base | 0xFF

	return base, hi
}

// Encode3D packs 3D spatial coordinates into a 64-bit Z-order Morton curve.
func (coder *MortonCoder) Encode3D(x, y, z uint32) uint64 {
	return part1by2(uint64(x)) | (part1by2(uint64(y)) << 1) | (part1by2(uint64(z)) << 2)
}

func part1by2(value uint64) uint64 {
	value &= 0x1fffff
	value = (value | (value << 32)) & 0x1f00000000ffff
	value = (value | (value << 16)) & 0x1f0000ff0000ff
	value = (value | (value << 8)) & 0x100f00f00f00f00f
	value = (value | (value << 4)) & 0x10c30c30c30c30c3
	value = (value | (value << 2)) & 0x1249249249249249
	return value
}

// Decode3D unpacks a 64-bit Z-order Morton curve into 3D spatial coordinates.
func (coder *MortonCoder) Decode3D(morton uint64) (x, y, z uint32) {
	return uint32(compact1by2(morton)), uint32(compact1by2(morton >> 1)), uint32(compact1by2(morton >> 2))
}

func compact1by2(value uint64) uint64 {
	value &= 0x1249249249249249
	value = (value ^ (value >> 2)) & 0x10c30c30c30c30c3
	value = (value ^ (value >> 4)) & 0x100f00f00f00f00f
	value = (value ^ (value >> 8)) & 0x1f0000ff0000ff
	value = (value ^ (value >> 16)) & 0x1f00000000ffff
	value = (value ^ (value >> 32)) & 0x1fffff
	return value
}

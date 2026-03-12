package process

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
Pack packs Symbol identity and absolute Position into a 64-bit key.
Layout (MSB→LSB):[24 zero bits | 8 bits Symbol | 32 bits absolute Pos].
Grouping by Symbol keeps same-byte neighborhoods contiguous in the LSBs while
the position remains a unique replay address across sequencer resets.
*/
func (coder *MortonCoder) Pack(pos uint32, symbol byte) uint64 {
	return (uint64(symbol) << 32) | uint64(pos)
}

/*
Unpack unpacks the 64-bit key back into absolute Position and Symbol identity.
*/
func (coder *MortonCoder) Unpack(morton uint64) (uint32, byte) {
	symbol := byte((morton >> 32) & 0xFF)
	pos := uint32(morton & 0xFFFFFFFF)

	return pos, symbol
}

/*
Encode4D packs four hierarchical dimensions into a 48-bit key within uint64.

Layout (MSB→LSB):
  [16 unused | SampleIdx(16) | SequenceIdx(8) | PosIdx(16) | Symbol(8)]

SampleIdx resets per dataset. SequenceIdx resets per sample. PosIdx resets
per sequence. This ordering puts all entries for a given sample in a
contiguous range, enabling trie traversal within a sample.
*/
func (coder *MortonCoder) Encode4D(sampleIdx uint16, sequenceIdx uint8, posIdx uint16, symbol byte) uint64 {
	return (uint64(sampleIdx) << 32) |
		(uint64(sequenceIdx) << 24) |
		(uint64(posIdx) << 8) |
		uint64(symbol)
}

/*
Decode4D unpacks the 48-bit key back into its four dimensions.
*/
func (coder *MortonCoder) Decode4D(key uint64) (sampleIdx uint16, sequenceIdx uint8, posIdx uint16, symbol byte) {
	sampleIdx = uint16((key >> 32) & 0xFFFF)
	sequenceIdx = uint8((key >> 24) & 0xFF)
	posIdx = uint16((key >> 8) & 0xFFFF)
	symbol = byte(key & 0xFF)

	return
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

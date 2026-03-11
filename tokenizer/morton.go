package tokenizer

import (
	"github.com/theapemachine/six/data"
)

/*
MortonCoder encodes multi-dimensional coordinates into a single uint64 key
for sorted storage in the LSM. Supports both the legacy 2D layout and the
hierarchical 4D layout.
*/
type MortonCoder struct {
	cellSize float64
	offset   float64
}

/*
NewMortonCoder creates a new Morton code encoder and decoder.
*/
func NewMortonCoder() *MortonCoder {
	return &MortonCoder{}
}

/*
Encode packs Symbol identity and absolute Position into a 64-bit Morton key.
Layout (MSB→LSB):[24 zero bits | 8 bits Symbol | 32 bits absolute Pos].
Grouping by Symbol keeps same-byte neighborhoods contiguous in the LSBs while
the position remains a unique replay address across sequencer resets.
*/
func (coder *MortonCoder) Encode(pos uint32, symbol byte) uint64 {
	return (uint64(symbol) << 32) | uint64(pos)
}

/*
Decode unpacks the 64-bit Morton key back into absolute Position and Symbol identity.
*/
func (coder *MortonCoder) Decode(morton uint64) (uint32, byte) {
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

/*
ChordToBytes returns chord.Bytes() — ChordBlocks×8 bytes big-endian.
*/
func (coder *MortonCoder) ChordToBytes(chord data.Chord) []byte {
	return chord.Bytes()
}

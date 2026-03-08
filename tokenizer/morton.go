package tokenizer

import (
	"github.com/theapemachine/six/data"
)

/*
Morton is a Morton code encoder and decoder.
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
ChordToBytes returns chord.Bytes() — ChordBlocks×8 bytes big-endian. Delegates to data.Chord.
*/
func (coder *MortonCoder) ChordToBytes(chord data.Chord) []byte {
	buf := chord.Bytes()

	return buf
}

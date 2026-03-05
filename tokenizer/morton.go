package tokenizer

import (
	"encoding/binary"

	config "github.com/theapemachine/six/core"
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
Encode packs Z-depth (scale), Symbol identity, and Position into a 64-bit Morton key.
Layout (MSB→LSB):[8 bits Z | 24 bits Symbol | 32 bits Pos].
Grouping by Z and Symbol allows perfectly contiguous sequence querying in the LSBs.
*/
func (coder *MortonCoder) Encode(z uint8, pos uint32, symbol byte) uint64 {
	return (uint64(z) << 56) | (uint64(symbol) << 32) | uint64(pos)
}

/*
Decode unpacks the 64-bit morton key back into Z-depth, Position, and Symbol identity.
*/
func (coder *MortonCoder) Decode(morton uint64) (uint8, uint32, byte) {
	z := uint8(morton >> 56)
	symbol := byte((morton >> 32) & 0xFFFFFF)
	pos := uint32(morton & 0xFFFFFFFF)

	return z, pos, symbol
}

/*
ChordToBytes encodes a Chord as core.ChordBlocks×8 bytes big-endian.
*/
func (coder *MortonCoder) ChordToBytes(chord data.Chord) []byte {
	buf := make([]byte, config.Numeric.ChordBlocks*8)

	for i := range config.Numeric.ChordBlocks {
		binary.BigEndian.PutUint64(buf[i*8:], uint64(chord.Bytes()[i]))
	}

	return buf
}

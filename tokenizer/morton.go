package tokenizer

import (
	"encoding/binary"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/numeric"
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
Encode packs byte, position, and Z-depth into a 64-bit Morton key.
Layout (MSB→LSB): [8 bits Z | 32 bits Pos | 24 bits Byte].
Z=0 is Bedrock (permanent); Z≥1 is Workbench (hypothesis). Z-Annealing
is an O(1) bit-flip: AnnealKey clears the top 8 bits to crystallize.
*/
func (coder *MortonCoder) Encode(symbol byte, pos uint32) uint64 {
	return (uint64(symbol) << 24) | uint64(pos)
}

/*
Decode unpacks the 64-bit morton key back into byte, pos, and zDepth.
*/
func (coder *MortonCoder) Decode(morton uint64) (byte, uint32) {
	return byte(morton & 0xFF), uint32((morton >> 24) & 0xFFFFFFFF)
}

/*
ChordToBytes encodes a Chord as core.ChordBlocks×8 bytes big-endian.
*/
func (coder *MortonCoder) ChordToBytes(chord data.Chord) []byte {
	buf := make([]byte, numeric.NSymbols*8)
	
	for i := range numeric.NSymbols {
		binary.BigEndian.PutUint64(buf[i*8:], chord[i])
	}
	
	return buf
}

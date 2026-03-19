package telemetry

import (
	"encoding/binary"
	"math"
)

/*
AppendBinary encodes the Event into dst as a flat binary frame.
Reuse dst[:0] across calls to avoid allocation entirely.

Layout (all little-endian):

	[len-prefixed strings] Component, Action, State, ChunkText,
	                        Stage, Message, ResultText, Outcome
	[23 × int32]           fixed numeric fields in declaration order
	[2 × float64]          Density, Theta
	[2 × uint8]            Advanced, Stable
	[3 × count-prefixed]   ActiveBits, MatchBits, CancelBits
*/
func (ev *Event) AppendBinary(dst []byte) []byte {
	dst = appendStr(dst, ev.Component)
	dst = appendStr(dst, ev.Action)

	d := &ev.Data

	dst = appendStr(dst, d.State)
	dst = appendStr(dst, d.ChunkText)
	dst = appendStr(dst, d.Stage)
	dst = appendStr(dst, d.Message)
	dst = appendStr(dst, d.ResultText)
	dst = appendStr(dst, d.Outcome)

	dst = appendI32(dst, d.ValueID)
	dst = appendI32(dst, d.Bin)
	dst = appendI32(dst, d.Residue)
	dst = appendI32(dst, d.Left)
	dst = appendI32(dst, d.Right)
	dst = appendI32(dst, d.Pos)
	dst = appendI32(dst, d.Paths)
	dst = appendI32(dst, d.Chunks)
	dst = appendI32(dst, d.Edges)
	dst = appendI32(dst, d.Level)
	dst = appendI32(dst, d.ParentBin)
	dst = appendI32(dst, d.ChildCount)
	dst = appendI32(dst, d.EdgeCount)
	dst = appendI32(dst, d.PathCount)
	dst = appendI32(dst, d.WavefrontEnergy)
	dst = appendI32(dst, d.EntryCount)
	dst = appendI32(dst, d.Step)
	dst = appendI32(dst, d.MaxSteps)
	dst = appendI32(dst, d.CandidateCount)
	dst = appendI32(dst, d.BestIndex)
	dst = appendI32(dst, d.PreResidue)
	dst = appendI32(dst, d.PostResidue)
	dst = appendI32(dst, d.SpanSize)

	dst = appendF64(dst, d.Density)
	dst = appendF64(dst, d.Theta)

	dst = appendBool(dst, d.Advanced)
	dst = appendBool(dst, d.Stable)

	dst = appendInts(dst, d.ActiveBits)
	dst = appendInts(dst, d.MatchBits)
	dst = appendInts(dst, d.CancelBits)

	return dst
}

/*
DecodeBinary reconstructs an Event from a binary frame produced by AppendBinary.
Lives on the cold path (visualizer process) so string allocations are acceptable.
*/
func DecodeBinary(buf []byte) Event {
	var ev Event
	off := 0

	ev.Component, off = readStr(buf, off)
	ev.Action, off = readStr(buf, off)

	ev.Data.State, off = readStr(buf, off)
	ev.Data.ChunkText, off = readStr(buf, off)
	ev.Data.Stage, off = readStr(buf, off)
	ev.Data.Message, off = readStr(buf, off)
	ev.Data.ResultText, off = readStr(buf, off)
	ev.Data.Outcome, off = readStr(buf, off)

	ev.Data.ValueID, off = readI32(buf, off)
	ev.Data.Bin, off = readI32(buf, off)
	ev.Data.Residue, off = readI32(buf, off)
	ev.Data.Left, off = readI32(buf, off)
	ev.Data.Right, off = readI32(buf, off)
	ev.Data.Pos, off = readI32(buf, off)
	ev.Data.Paths, off = readI32(buf, off)
	ev.Data.Chunks, off = readI32(buf, off)
	ev.Data.Edges, off = readI32(buf, off)
	ev.Data.Level, off = readI32(buf, off)
	ev.Data.ParentBin, off = readI32(buf, off)
	ev.Data.ChildCount, off = readI32(buf, off)
	ev.Data.EdgeCount, off = readI32(buf, off)
	ev.Data.PathCount, off = readI32(buf, off)
	ev.Data.WavefrontEnergy, off = readI32(buf, off)
	ev.Data.EntryCount, off = readI32(buf, off)
	ev.Data.Step, off = readI32(buf, off)
	ev.Data.MaxSteps, off = readI32(buf, off)
	ev.Data.CandidateCount, off = readI32(buf, off)
	ev.Data.BestIndex, off = readI32(buf, off)
	ev.Data.PreResidue, off = readI32(buf, off)
	ev.Data.PostResidue, off = readI32(buf, off)
	ev.Data.SpanSize, off = readI32(buf, off)

	ev.Data.Density, off = readF64(buf, off)
	ev.Data.Theta, off = readF64(buf, off)

	ev.Data.Advanced, off = readBool(buf, off)
	ev.Data.Stable, off = readBool(buf, off)

	ev.Data.ActiveBits, off = readInts(buf, off)
	ev.Data.MatchBits, off = readInts(buf, off)
	ev.Data.CancelBits, off = readInts(buf, off)

	_ = off
	return ev
}

func appendStr(dst []byte, s string) []byte {
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(s)))
	return append(dst, s...)
}

func appendI32(dst []byte, v int) []byte {
	return binary.LittleEndian.AppendUint32(dst, uint32(int32(v)))
}

func appendF64(dst []byte, v float64) []byte {
	return binary.LittleEndian.AppendUint64(dst, math.Float64bits(v))
}

func appendBool(dst []byte, v bool) []byte {
	if v {
		return append(dst, 1)
	}

	return append(dst, 0)
}

func appendInts(dst []byte, s []int) []byte {
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(s)))

	for _, v := range s {
		dst = binary.LittleEndian.AppendUint32(dst, uint32(int32(v)))
	}

	return dst
}

func readStr(buf []byte, off int) (string, int) {
	n := int(binary.LittleEndian.Uint16(buf[off:]))
	off += 2

	return string(buf[off : off+n]), off + n
}

func readI32(buf []byte, off int) (int, int) {
	return int(int32(binary.LittleEndian.Uint32(buf[off:]))), off + 4
}

func readF64(buf []byte, off int) (float64, int) {
	return math.Float64frombits(binary.LittleEndian.Uint64(buf[off:])), off + 8
}

func readBool(buf []byte, off int) (bool, int) {
	return buf[off] != 0, off + 1
}

func readInts(buf []byte, off int) ([]int, int) {
	n := int(binary.LittleEndian.Uint16(buf[off:]))
	off += 2

	s := make([]int, n)

	for i := range s {
		s[i] = int(int32(binary.LittleEndian.Uint32(buf[off:])))
		off += 4
	}

	return s, off
}

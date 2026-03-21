package sequencer

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
BitwiseHealer buffers fragmented sequencer output and heals false boundaries by
discovering exact repeated spans and resegmenting around those anchors.
Each byte is stored as a BaseValue so exact identity is preserved through AND.
*/
type BitwiseHealer struct {
	state  *errnie.State
	buffer bytes.Buffer
	values [][]primitive.Value
}

/*
NewBitwiseHealer creates a fixed-capacity buffer for fragmented sequences.
*/
func NewBitwiseHealer() *BitwiseHealer {
	return &BitwiseHealer{
		state:  errnie.NewState("sequencer/bitwise/healer"),
		values: make([][]primitive.Value, 0),
	}
}

/*
Write appends a fragmented sequence to the buffer. Each fragment is a group
of per-byte BaseValues.
*/
func (bitwise *BitwiseHealer) Write(b byte, isBoundary bool) {
	bitwise.buffer.WriteByte(b)

	if isBoundary {
		chunk := make([]primitive.Value, 0, bitwise.buffer.Len())

		for _, value := range bitwise.buffer.Bytes() {
			chunk = append(chunk, primitive.BaseValue(value))
		}

		bitwise.values = append(bitwise.values, chunk)
		bitwise.buffer.Reset()
	}
}

/*
Flush emits any remaining bytes in the buffer as a final chunk.
Call after feeding all input when the stream ends.
*/
func (bitwise *BitwiseHealer) Flush() ([][]byte, error) {
	if bitwise.buffer.Len() == 0 {
		return nil, nil
	}

	chunk := make([]primitive.Value, 0, bitwise.buffer.Len())

	for _, value := range bitwise.buffer.Bytes() {
		chunk = append(chunk, primitive.BaseValue(value))
	}

	bitwise.values = append(bitwise.values, chunk)
	bitwise.buffer.Reset()

	return bitwise.Heal()
}

/*
Heal discovers exact repeated anchors in the buffered Value stream and rebuilds
the segmentation from derived cut positions. This removes false internal cuts
without relying on text-specific cues.
*/
func (bitwise *BitwiseHealer) Heal() ([][]byte, error) {
	if len(bitwise.values) == 0 {
		return nil, nil
	}

	raw := bitwise.flatten(bitwise.values)

	if len(raw) == 0 {
		bitwise.values = bitwise.values[:0]

		return nil, nil
	}

	boundaries := bitwise.collectBoundaries(bitwise.values)
	cuts, selectErr := bitwise.selectCuts(raw, boundaries)
	if selectErr != nil {
		return nil, selectErr
	}

	cuts = bitwise.mergeOnlyCuts(cuts, boundaries)

	if len(cuts) <= 2 {
		result, decodeErr := bitwise.decodeChunks(bitwise.values)
		bitwise.values = bitwise.values[:0]

		return result, decodeErr
	}

	result, segmentErr := bitwise.segment(raw, cuts)
	bitwise.values = bitwise.values[:0]

	return result, segmentErr
}

/*
flatten concatenates multiple chunk slices into a single Value slice.
*/
func (bitwise *BitwiseHealer) flatten(chunks [][]primitive.Value) []primitive.Value {
	var result []primitive.Value

	for _, chunk := range chunks {
		result = append(result, chunk...)
	}

	return result
}

/*
collectBoundaries records the original fragment boundaries in the flattened
stream.
*/
func (bitwise *BitwiseHealer) collectBoundaries(chunks [][]primitive.Value) map[int]struct{} {
	boundaries := map[int]struct{}{
		0: {},
	}

	offset := 0

	for _, chunk := range chunks {
		offset += len(chunk)
		boundaries[offset] = struct{}{}
	}

	return boundaries
}

/*
selectCuts finds repeated exact spans and converts them into cut positions.
Crossing and fully aligned spans cut on both edges. Start-only spans cut on
their supported edge only.
*/
func (bitwise *BitwiseHealer) selectCuts(raw []primitive.Value, boundaries map[int]struct{}) ([]int, error) {
	type candidateEntry struct {
		length int
		starts []int
	}

	candidates := make(map[string]struct {
		length    int
		starts    map[int]struct{}
		crossing  bool
		startEdge bool
		endEdge   bool
	})

	for leftStart := range raw {
		for rightStart := leftStart + 1; rightStart < len(raw); rightStart++ {
			commonLength := bitwise.sharedPrefix(raw, leftStart, rightStart)

			if commonLength < 2 {
				continue
			}

			span := raw[leftStart : leftStart+commonLength]
			key, sigErr := bitwise.signature(span)
			if sigErr != nil {
				return nil, sigErr
			}

			candidate := candidates[key]

			if candidate.length == 0 {
				candidate.length = commonLength
				candidate.starts = make(map[int]struct{})
			}

			candidate.starts[leftStart] = struct{}{}
			candidate.starts[rightStart] = struct{}{}

			candidate.crossing = candidate.crossing ||
				bitwise.crossesBoundary(leftStart, leftStart+commonLength, boundaries) ||
				bitwise.crossesBoundary(rightStart, rightStart+commonLength, boundaries)

			candidate.startEdge = candidate.startEdge ||
				(bitwise.isBoundary(leftStart, boundaries) && bitwise.isBoundary(rightStart, boundaries))

			candidate.endEdge = candidate.endEdge ||
				(bitwise.isBoundary(leftStart+commonLength, boundaries) &&
					bitwise.isBoundary(rightStart+commonLength, boundaries))

			candidates[key] = candidate
		}
	}

	longestBothPerStarts := make(map[string]int)
	longestStartPerStarts := make(map[string]int)

	for _, candidate := range candidates {
		if len(candidate.starts) < 2 {
			continue
		}

		startKey := bitwise.startsKey(candidate.starts)

		switch {
		case candidate.crossing || (candidate.startEdge && candidate.endEdge):
			if candidate.length > longestBothPerStarts[startKey] {
				longestBothPerStarts[startKey] = candidate.length
			}

		case candidate.startEdge:
			if candidate.length > longestStartPerStarts[startKey] {
				longestStartPerStarts[startKey] = candidate.length
			}
		}
	}

	var bothCandidates []candidateEntry
	var startCandidates []candidateEntry

	for _, candidate := range candidates {
		if len(candidate.starts) < 2 {
			continue
		}

		startKey := bitwise.startsKey(candidate.starts)
		starts := bitwise.sortedStarts(candidate.starts)

		switch {
		case candidate.crossing || (candidate.startEdge && candidate.endEdge):
			if candidate.length == longestBothPerStarts[startKey] {
				bothCandidates = append(bothCandidates, candidateEntry{
					length: candidate.length,
					starts: starts,
				})
			}

		case candidate.startEdge:
			if candidate.length == longestStartPerStarts[startKey] {
				startCandidates = append(startCandidates, candidateEntry{
					length: candidate.length,
					starts: starts,
				})
			}
		}
	}

	sort.Slice(bothCandidates, func(leftIndex, rightIndex int) bool {
		left := bothCandidates[leftIndex]
		right := bothCandidates[rightIndex]

		leftScore := left.length * len(left.starts)
		rightScore := right.length * len(right.starts)

		if leftScore != rightScore {
			return leftScore > rightScore
		}

		if left.length != right.length {
			return left.length > right.length
		}

		return len(left.starts) > len(right.starts)
	})

	var acceptedAnchors [][2]int
	occupied := make([]bool, len(raw))

	for _, candidate := range bothCandidates {
		var accepted [][2]int

		for _, start := range candidate.starts {
			end := start + candidate.length

			if bitwise.overlaps(occupied, start, end) {
				continue
			}

			accepted = append(accepted, [2]int{start, end})
		}

		if len(accepted) < 2 {
			continue
		}

		for _, anchor := range accepted {
			for index := anchor[0]; index < anchor[1]; index++ {
				occupied[index] = true
			}

			acceptedAnchors = append(acceptedAnchors, anchor)
		}
	}

	cutSet := map[int]struct{}{
		0:        {},
		len(raw): {},
	}

	for _, anchor := range acceptedAnchors {
		cutSet[anchor[0]] = struct{}{}
		cutSet[anchor[1]] = struct{}{}
	}

	for _, candidate := range startCandidates {
		for _, start := range candidate.starts {
			if bitwise.cutsInterior(start, acceptedAnchors) {
				continue
			}

			cutSet[start] = struct{}{}
		}
	}

	positions := make([]int, 0, len(cutSet))

	for position := range cutSet {
		positions = append(positions, position)
	}

	sort.Ints(positions)

	return positions, nil
}

/*
mergeOnlyCuts filters derived cuts to original fragment boundaries so healing
can only merge buffered fragments and never split them.
*/
func (bitwise *BitwiseHealer) mergeOnlyCuts(cuts []int, boundaries map[int]struct{}) []int {
	filtered := make([]int, 0, len(cuts))

	for _, cut := range cuts {
		if !bitwise.isBoundary(cut, boundaries) {
			continue
		}

		filtered = append(filtered, cut)
	}

	return filtered
}

/*
segment rebuilds the stream from derived cut positions.
*/
func (bitwise *BitwiseHealer) segment(raw []primitive.Value, cuts []int) ([][]byte, error) {
	var result [][]byte

	for index := 0; index < len(cuts)-1; index++ {
		start := cuts[index]
		end := cuts[index+1]

		if start == end {
			continue
		}

		chunk, decodeErr := bitwise.decode(raw[start:end])
		if decodeErr != nil {
			return nil, decodeErr
		}

		result = append(result, chunk)
	}

	return result, nil
}

/*
decodeChunks decodes the original chunk layout unchanged.
*/
func (bitwise *BitwiseHealer) decodeChunks(chunks [][]primitive.Value) ([][]byte, error) {
	result := make([][]byte, 0, len(chunks))

	for _, chunk := range chunks {
		decoded, decodeErr := bitwise.decode(chunk)
		if decodeErr != nil {
			return nil, decodeErr
		}

		result = append(result, decoded)
	}

	return result, nil
}

/*
sharedPrefix returns the exact common span length from two start positions.
*/
func (bitwise *BitwiseHealer) sharedPrefix(raw []primitive.Value, leftStart, rightStart int) int {
	limit := len(raw) - rightStart

	if remaining := len(raw) - leftStart; remaining < limit {
		limit = remaining
	}

	length := 0

	for offset := 0; offset < limit; offset++ {
		if !bitwise.matchBytes(raw[leftStart+offset], raw[rightStart+offset]) {
			break
		}

		length++
	}

	return length
}

/*
signature encodes a Value span into a stable exact key.
*/
func (bitwise *BitwiseHealer) signature(values []primitive.Value) (string, error) {
	decoded, err := bitwise.decode(values)
	if err != nil {
		return "", err
	}

	return string(decoded), nil
}

/*
startsKey converts a start-position set into a stable key.
*/
func (bitwise *BitwiseHealer) startsKey(starts map[int]struct{}) string {
	sorted := bitwise.sortedStarts(starts)
	var builder strings.Builder

	for _, start := range sorted {
		builder.WriteString(strconv.Itoa(start))
		builder.WriteByte('|')
	}

	return builder.String()
}

/*
sortedStarts returns the candidate start positions in ascending order.
*/
func (bitwise *BitwiseHealer) sortedStarts(starts map[int]struct{}) []int {
	result := make([]int, 0, len(starts))

	for start := range starts {
		result = append(result, start)
	}

	sort.Ints(result)

	return result
}

/*
isBoundary checks whether a position is an original fragment edge.
*/
func (bitwise *BitwiseHealer) isBoundary(position int, boundaries map[int]struct{}) bool {
	_, ok := boundaries[position]

	return ok
}

/*
crossesBoundary reports whether a span contains an internal original boundary.
*/
func (bitwise *BitwiseHealer) crossesBoundary(start, end int, boundaries map[int]struct{}) bool {
	for boundary := range boundaries {
		if boundary <= start || boundary >= end {
			continue
		}

		return true
	}

	return false
}

/*
cutsInterior reports whether a cut would split an accepted two-edge anchor.
*/
func (bitwise *BitwiseHealer) cutsInterior(position int, anchors [][2]int) bool {
	for _, anchor := range anchors {
		if position > anchor[0] && position < anchor[1] {
			return true
		}
	}

	return false
}

/*
overlaps reports whether a candidate anchor collides with an already selected
anchor.
*/
func (bitwise *BitwiseHealer) overlaps(occupied []bool, start, end int) bool {
	for idx := start; idx < end; idx++ {
		if occupied[idx] {
			return true
		}
	}

	return false
}

/*
coreKey extracts the GF(8191) core field (CoreBlocks words) as a compact map key.
*/
func coreKey(value primitive.Value) [config.CoreBlocks]uint64 {
	var key [config.CoreBlocks]uint64

	for index := range config.CoreBlocks {
		key[index] = value.Block(index)
	}

	return key
}

/*
baseValueLookup maps each byte's BaseValue core pattern to the byte it
represents. Built once at init; O(1) decode per value thereafter.
*/
var baseValueLookup = func() map[[config.CoreBlocks]uint64]byte {
	table := make(map[[config.CoreBlocks]uint64]byte, 256)

	for symbol := range 256 {
		table[coreKey(primitive.BaseValue(byte(symbol)))] = byte(symbol)
	}

	return table
}()

/*
decode converts a slice of per-byte BaseValues back to a byte slice via the
precomputed lookup table.
*/
func (bitwise *BitwiseHealer) decode(values []primitive.Value) ([]byte, error) {
	result := make([]byte, len(values))

	for index, value := range values {
		key := coreKey(value)
		symbol, ok := baseValueLookup[key]
		if !ok {
			return nil, fmt.Errorf(
				"sequencer/bitwise: unknown BaseValue core at index %d (key %v)",
				index,
				key,
			)
		}

		result[index] = symbol
	}

	return result, nil
}

/*
matchBytes checks if two byte-Values represent the same byte by comparing
each GF(8191) core block. No allocation, no AND.
*/
func (bitwise *BitwiseHealer) matchBytes(valA, valB primitive.Value) bool {
	for index := range config.CoreBlocks {
		if valA.Block(index) != valB.Block(index) {
			return false
		}
	}

	return true
}

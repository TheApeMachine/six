package sequencer

import (
	"slices"

	"github.com/theapemachine/six/pkg/store/data"
)

/*
BitwiseHealer buffers fragmented sequencer output and heals false boundaries by
discovering exact shared spans through bitwise overlap. Shared spans become
anchors; the bytes between anchors are re-coalesced into larger chunks.
*/
type BitwiseHealer struct {
	buffer     [][][]data.Value
	bufferSize int
}

/*
NewBitwiseHealer creates a fixed-capacity buffer for fragmented sequences.
*/
func NewBitwiseHealer() *BitwiseHealer {
	return &BitwiseHealer{
		buffer:     make([][][]data.Value, 0, 1024),
		bufferSize: 1024,
	}
}

/*
Capacity returns the maximum number of buffered sequences.
*/
func (bitwise *BitwiseHealer) Capacity() int {
	return bitwise.bufferSize
}

/*
Write appends one fragmented sequence to the sliding buffer.
*/
func (bitwise *BitwiseHealer) Write(sequence [][]data.Value) {
	if len(bitwise.buffer) >= bitwise.bufferSize {
		bitwise.buffer = bitwise.buffer[1:]
	}

	bitwise.buffer = append(bitwise.buffer, bitwise.cloneSequence(sequence))
}

/*
Heal groups buffered sequences by strong shared spans, extracts common anchors
within each group, and re-chunks the raw bytes between those anchors.
*/
func (bitwise *BitwiseHealer) Heal() [][][]data.Value {
	if len(bitwise.buffer) == 0 {
		return nil
	}

	healed := bitwise.cloneBuffer()

	if len(bitwise.buffer) < 2 {
		return healed
	}

	flattened := make([][]data.Value, len(bitwise.buffer))
	boundaries := make([][]int, len(bitwise.buffer))

	for i, sequence := range bitwise.buffer {
		flattened[i], boundaries[i] = bitwise.flatten(sequence)
	}

	for _, component := range bitwise.connectedComponents(flattened, boundaries) {
		if len(component) < 2 {
			continue
		}

		componentSeqs := make([][]data.Value, len(component))
		componentBoundaries := make([][]int, len(component))

		for i, idx := range component {
			componentSeqs[i] = flattened[idx]
			componentBoundaries[i] = boundaries[idx]
		}

		anchors := bitwise.findSharedAnchors(
			componentSeqs,
			componentBoundaries,
			bitwise.anchorFloor(componentSeqs),
		)

		if len(anchors) == 0 {
			continue
		}

		for i, idx := range component {
			healed[idx] = bitwise.rechunk(componentSeqs[i], anchors, i)
		}
	}

	return healed
}

/*
Components returns the current overlap-connected sequence groups.
*/
func (bitwise *BitwiseHealer) Components() [][]int {
	if len(bitwise.buffer) == 0 {
		return nil
	}

	flattened := make([][]data.Value, len(bitwise.buffer))
	boundaries := make([][]int, len(bitwise.buffer))

	for i, sequence := range bitwise.buffer {
		flattened[i], boundaries[i] = bitwise.flatten(sequence)
	}

	return bitwise.connectedComponents(flattened, boundaries)
}

func (bitwise *BitwiseHealer) cloneBuffer() [][][]data.Value {
	buffer := make([][][]data.Value, len(bitwise.buffer))

	for i, sequence := range bitwise.buffer {
		buffer[i] = bitwise.cloneSequence(sequence)
	}

	return buffer
}

func (bitwise *BitwiseHealer) cloneSequence(sequence [][]data.Value) [][]data.Value {
	cloned := make([][]data.Value, len(sequence))

	for i, fragment := range sequence {
		cloned[i] = append([]data.Value(nil), fragment...)
	}

	return cloned
}

func (bitwise *BitwiseHealer) flatten(sequence [][]data.Value) ([]data.Value, []int) {
	total := 0
	boundaries := []int{0}

	for _, fragment := range sequence {
		total += len(fragment)
		boundaries = append(boundaries, total)
	}

	flattened := make([]data.Value, 0, total)

	for _, fragment := range sequence {
		flattened = append(flattened, fragment...)
	}

	return flattened, boundaries
}

func (bitwise *BitwiseHealer) connectedComponents(
	sequences [][]data.Value,
	boundaries [][]int,
) [][]int {
	visited := make([]bool, len(sequences))
	components := make([][]int, 0, len(sequences))

	for i := range sequences {
		if visited[i] {
			continue
		}

		component := []int{}
		queue := []int{i}
		visited[i] = true

		for len(queue) > 0 {
			idx := queue[0]
			queue = queue[1:]
			component = append(component, idx)

			for other := range sequences {
				if visited[other] {
					continue
				}

				if bitwise.shareClusterAnchor(
					sequences[idx], boundaries[idx],
					sequences[other], boundaries[other],
				) {
					visited[other] = true
					queue = append(queue, other)
				}
			}
		}

		components = append(components, component)
	}

	return components
}

func (bitwise *BitwiseHealer) shareClusterAnchor(
	left []data.Value,
	leftBoundaries []int,
	right []data.Value,
	rightBoundaries []int,
) bool {
	_, ok := bitwise.bestSharedSpan(
		[][]data.Value{left, right},
		[][]int{leftBoundaries, rightBoundaries},
		bitwise.clusterFloor(left, right),
	)

	return ok
}

func (bitwise *BitwiseHealer) findSharedAnchors(
	sequences [][]data.Value,
	boundaries [][]int,
	minimum int,
) [][][2]int {
	anchor, ok := bitwise.bestSharedSpan(sequences, boundaries, minimum)
	if !ok {
		return nil
	}

	prefixSeqs := make([][]data.Value, len(sequences))
	prefixBoundaries := make([][]int, len(sequences))
	suffixSeqs := make([][]data.Value, len(sequences))
	suffixBoundaries := make([][]int, len(sequences))

	for i, position := range anchor {
		prefixSeqs[i] = append([]data.Value(nil), sequences[i][:position[0]]...)
		prefixBoundaries[i] = bitwise.sliceBoundaries(boundaries[i], 0, position[0])

		suffixSeqs[i] = append([]data.Value(nil), sequences[i][position[1]:]...)
		suffixBoundaries[i] = bitwise.sliceBoundaries(
			boundaries[i],
			position[1],
			len(sequences[i]),
		)
	}

	prefix := bitwise.findSharedAnchors(
		prefixSeqs,
		prefixBoundaries,
		bitwise.anchorFloor(prefixSeqs),
	)
	suffix := bitwise.findSharedAnchors(
		suffixSeqs,
		suffixBoundaries,
		bitwise.anchorFloor(suffixSeqs),
	)

	anchors := make([][][2]int, 0, len(prefix)+1+len(suffix))
	anchors = append(anchors, prefix...)
	anchors = append(anchors, anchor)

	for _, suffixAnchor := range suffix {
		shifted := make([][2]int, len(suffixAnchor))

		for i, position := range suffixAnchor {
			shifted[i] = [2]int{
				position[0] + anchor[i][1],
				position[1] + anchor[i][1],
			}
		}

		anchors = append(anchors, shifted)
	}

	return anchors
}

func (bitwise *BitwiseHealer) bestSharedSpan(
	sequences [][]data.Value,
	boundaries [][]int,
	minimum int,
) ([][2]int, bool) {
	if len(sequences) < 2 || minimum < 2 {
		return nil, false
	}

	base := sequences[0]
	if len(base) < minimum {
		return nil, false
	}

	bestLength := 0
	bestScore := 0
	bestWeight := 0
	var best [][2]int

	for start := 0; start <= len(base)-minimum; start++ {
		for end := len(base); end >= start+minimum; end-- {
			length := end - start

			if length < bestLength {
				break
			}

			span := base[start:end]
			positions := make([][2]int, len(sequences))
			positions[0] = [2]int{start, end}

			totalScore := bitwise.occurrenceScore(start, end, boundaries[0])
			matched := true

			for i := 1; i < len(sequences); i++ {
				position, ok, score := bitwise.findOccurrence(
					span,
					sequences[i],
					boundaries[i],
				)
				if !ok {
					matched = false
					break
				}

				positions[i] = position
				totalScore += score
			}

			if !matched || totalScore == 0 {
				continue
			}

			weight := totalScore*2 + length

			if weight > bestWeight ||
				(weight == bestWeight && length > bestLength) ||
				(weight == bestWeight && length == bestLength && totalScore > bestScore) {
				bestWeight = weight
				bestLength = length
				bestScore = totalScore
				best = positions
			}
		}
	}

	return best, bestLength > 0
}

func (bitwise *BitwiseHealer) findOccurrence(
	span []data.Value,
	sequence []data.Value,
	boundaries []int,
) ([2]int, bool, int) {
	if len(span) == 0 || len(sequence) < len(span) {
		return [2]int{}, false, 0
	}

	bestScore := -1
	bestPosition := [2]int{}
	found := false
	limit := len(sequence) - len(span)

	for start := 0; start <= limit; start++ {
		if !bitwise.matchSpan(span, sequence, start) {
			continue
		}

		end := start + len(span)
		score := bitwise.occurrenceScore(start, end, boundaries)

		if !found || score > bestScore {
			bestScore = score
			bestPosition = [2]int{start, end}
			found = true
		}
	}

	if !found {
		return [2]int{}, false, 0
	}

	return bestPosition, true, bestScore
}

func (bitwise *BitwiseHealer) matchSpan(
	span []data.Value,
	sequence []data.Value,
	offset int,
) bool {
	for i, value := range span {
		if !bitwise.matchValue(value, sequence[offset+i]) {
			return false
		}
	}

	return true
}

func (bitwise *BitwiseHealer) matchValue(left, right data.Value) bool {
	shared := left.AND(right)

	return shared.ActiveCount() == left.ActiveCount() &&
		shared.ActiveCount() == right.ActiveCount()
}

func (bitwise *BitwiseHealer) occurrenceScore(
	start int,
	end int,
	boundaries []int,
) int {
	alignedStart := slices.Contains(boundaries, start)
	alignedEnd := slices.Contains(boundaries, end)
	crossesBoundary := false

	for _, boundary := range boundaries {
		if start < boundary && boundary < end {
			crossesBoundary = true
			break
		}
	}

	score := 0

	if alignedStart && alignedEnd {
		score += 4
	}

	if crossesBoundary {
		score++

		if alignedStart {
			score += 2
		}

		if alignedEnd {
			score += 2
		}
	}

	return score
}

func (bitwise *BitwiseHealer) sliceBoundaries(
	boundaries []int,
	start int,
	end int,
) []int {
	if end <= start {
		return []int{0}
	}

	sliced := []int{0}

	for _, boundary := range boundaries {
		if start < boundary && boundary < end {
			sliced = append(sliced, boundary-start)
		}
	}

	if sliced[len(sliced)-1] != end-start {
		sliced = append(sliced, end-start)
	}

	return sliced
}

func (bitwise *BitwiseHealer) rechunk(
	sequence []data.Value,
	anchors [][][2]int,
	sequenceIndex int,
) [][]data.Value {
	chunks := make([][]data.Value, 0, len(anchors)*2+1)
	offset := 0

	for _, anchor := range anchors {
		start := anchor[sequenceIndex][0]
		end := anchor[sequenceIndex][1]

		if offset < start {
			chunks = append(chunks, append([]data.Value(nil), sequence[offset:start]...))
		}

		chunks = append(chunks, append([]data.Value(nil), sequence[start:end]...))
		offset = end
	}

	if offset < len(sequence) {
		chunks = append(chunks, append([]data.Value(nil), sequence[offset:]...))
	}

	return chunks
}

func (bitwise *BitwiseHealer) anchorFloor(sequences [][]data.Value) int {
	shortest := 0

	for _, sequence := range sequences {
		if len(sequence) == 0 {
			return 0
		}

		if shortest == 0 || len(sequence) < shortest {
			shortest = len(sequence)
		}
	}

	// Recursive healing can use a smaller floor because the outer split already
	// established the component. One quarter of the shortest sequence keeps the
	// anchor structural instead of collapsing into stray bigrams.
	floor := shortest / 4
	if floor < 2 {
		floor = 2
	}

	return floor
}

func (bitwise *BitwiseHealer) clusterFloor(
	left []data.Value,
	right []data.Value,
) int {
	shortest := len(left)
	if len(right) < shortest {
		shortest = len(right)
	}

	// Component formation uses a stricter floor than recursive healing so
	// generic short spans such as "e " do not fuse unrelated sequence families.
	floor := shortest / 3
	if floor < 2 {
		floor = 2
	}

	return floor
}

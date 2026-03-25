package cpu

import (
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type CancellationSpan struct {
	Start        int
	Length       int
	TouchesLeft  bool
	TouchesRight bool
}

type CancellationCandidate struct {
	LeftIndex    int
	RightIndex   int
	LeftValueID  uint64
	RightValueID uint64
	Span         CancellationSpan
}

func decodeBatchFrames(batch []byte) []primitive.Value {
	count := len(batch) / primitive.ByteSize
	values := make([]primitive.Value, 0, count)

	for offset := 0; offset+primitive.ByteSize <= len(batch); offset += primitive.ByteSize {
		frame := primitive.BytesToValue(batch[offset : offset+primitive.ByteSize])
		values = append(values, *frame)
	}

	return values
}

func strongestCancellation(values []primitive.Value) (CancellationCandidate, bool) {
	if len(values) < 2 {
		return CancellationCandidate{}, false
	}

	workers := max(1, min(len(values), runtime.NumCPU()-1))
	type result struct {
		candidate CancellationCandidate
		ok        bool
	}

	jobs := make(chan int, len(values))
	results := make(chan result, workers)

	for range workers {
		go func() {
			var best CancellationCandidate
			ok := false

			for i := range jobs {
				for j := i + 1; j < len(values); j++ {
					span := longestCancellationSpan(&values[i], &values[j])
					if span.Length == 0 {
						continue
					}

					candidate := CancellationCandidate{
						LeftIndex:    i,
						RightIndex:   j,
						LeftValueID:  values[i].ValueID(),
						RightValueID: values[j].ValueID(),
						Span:         span,
					}

					if !ok || betterCancellation(candidate, best) {
						best = candidate
						ok = true
					}
				}
			}

			results <- result{candidate: best, ok: ok}
		}()
	}

	for i := range len(values) {
		jobs <- i
	}
	close(jobs)

	var best CancellationCandidate
	ok := false
	for range workers {
		result := <-results
		if !result.ok {
			continue
		}
		if !ok || betterCancellation(result.candidate, best) {
			best = result.candidate
			ok = true
		}
	}

	return best, ok
}

func betterCancellation(a, b CancellationCandidate) bool {
	if a.Span.Length != b.Span.Length {
		return a.Span.Length > b.Span.Length
	}
	if a.Span.Start != b.Span.Start {
		return a.Span.Start < b.Span.Start
	}
	if a.LeftValueID != b.LeftValueID {
		return a.LeftValueID < b.LeftValueID
	}
	return a.RightValueID < b.RightValueID
}

func longestCancellationSpan(a, b *primitive.Value) CancellationSpan {
	best := CancellationSpan{}
	start := -1
	length := 0

	for i := 0; i < primitive.Region0TokenCount; i++ {
		if length+(primitive.Region0TokenCount-i) <= best.Length {
			break
		}

		leftToken := a[i]
		rightToken := b[i]
		if leftToken != 0 && leftToken == rightToken {
			if start == -1 {
				start = i
				length = 0
			}
			length++
			continue
		}

		if length > best.Length {
			best = CancellationSpan{
				Start:        start,
				Length:       length,
				TouchesLeft:  start == 0,
				TouchesRight: start+length == primitive.Region0TokenCount,
			}
		}
		start = -1
		length = 0
	}

	if length > best.Length {
		best = CancellationSpan{
			Start:        start,
			Length:       length,
			TouchesLeft:  start == 0,
			TouchesRight: start+length == primitive.Region0TokenCount,
		}
	}

	return best
}

func buildEmittedValue(
	left, right *primitive.Value,
	span CancellationSpan,
	valueID uint64,
) *primitive.Value {
	emitted := primitive.NewValue()
	next := 0

	for i := span.Start; i < span.Start+span.Length && next < primitive.Region0TokenCount; i++ {
		emitted.SetTokenID(next, left.TokenID(i))
		next++
	}

	next = appendRemainder(emitted, next, left, span)
	next = appendRemainder(emitted, next, right, span)

	emitted.SetValueID(valueID)
	emitted.SetPrevValueID(left.ValueID())
	emitted.SetNextValueID(right.ValueID())

	errnie.Trace(
		"compute.kernel.cpu.trie.buildEmittedValue",
		"emitted", emitted.TokenIDs(),
		"left", left.TokenIDs(),
		"right", right.TokenIDs(),
		"span", span,
		"valueID", valueID,
	)

	return emitted
}

func appendRemainder(
	dst *primitive.Value,
	next int,
	src *primitive.Value,
	span CancellationSpan,
) int {
	for i := 0; i < primitive.Region0TokenCount && next < primitive.Region0TokenCount; i++ {
		if i >= span.Start && i < span.Start+span.Length {
			continue
		}

		token := src.TokenID(i)
		if token == 0 {
			continue
		}

		dst.SetTokenID(next, token)
		next++
	}

	return next
}

/*
EncodeTrieBatch packs byte sequences into Region0 TokenIDs and IDs.
*/
func (backend *Backend) EncodeTrieBatch(
	sequences [][]byte, dst unsafe.Pointer, numValues uint32,
) {
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := uint32(0); v < numValues; v++ {
		value := (*primitive.Value)(unsafe.Pointer(&ds[v]))
		seq := sequences[v]

		for i, b := range seq {
			if i >= primitive.Region0TokenCount {
				break
			}
			value.SetTokenID(i, primitive.Tokenize(b, uint64(i)))
		}

		value.SetValueID(uint64(v + 1))
	}
}

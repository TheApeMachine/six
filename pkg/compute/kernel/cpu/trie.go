package cpu

import (
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type CancellationSpan struct {
	LeftStart  int
	RightStart int
	Length     int
}

type CancellationCandidate struct {
	LeftIndex    int
	RightIndex   int
	LeftValueID  uint64
	RightValueID uint64
	Span         CancellationSpan
}

func tokenLen(v *primitive.Value) int {
	for i := 0; i < primitive.Region0TokenCount; i++ {
		if v[i] == 0 {
			return i
		}
	}
	return primitive.Region0TokenCount
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
	if a.Span.LeftStart != b.Span.LeftStart {
		return a.Span.LeftStart < b.Span.LeftStart
	}
	if a.LeftValueID != b.LeftValueID {
		return a.LeftValueID < b.LeftValueID
	}
	return a.RightValueID < b.RightValueID
}

// longestCancellationSpan finds the longest contiguous run of matching
// byte-content tokens between a and b, allowing different starting offsets.
// Comparison uses only the byte portion (upper 32 bits) of each TokenID
// so that the same text matches regardless of its position in the sentence.
func longestCancellationSpan(a, b *primitive.Value) CancellationSpan {
	aLen := tokenLen(a)
	bLen := tokenLen(b)
	if aLen == 0 || bLen == 0 {
		return CancellationSpan{}
	}

	best := CancellationSpan{}

	for i := 0; i < aLen; i++ {
		for j := 0; j < bLen; j++ {
			if (a[i] >> 32) != (b[j] >> 32) {
				continue
			}
			length := 1
			for i+length < aLen && j+length < bLen &&
				(a[i+length]>>32) == (b[j+length]>>32) {
				length++
			}
			if length > best.Length {
				best = CancellationSpan{
					LeftStart:  i,
					RightStart: j,
					Length:     length,
				}
			}
		}
	}

	return best
}

// buildEmittedValues splits two colliding Values into a linked chain:
//   - shared: the common tokens (the label)
//   - leftRem: left's unique tokens, NextValueID → shared
//   - rightRem: right's unique tokens, NextValueID → shared
//
// All returned Values have StateSlotIndex set and valid IDs.
// The caller must not set IDs on the returned values — they are assigned here.
func buildEmittedValues(
	left, right *primitive.Value,
	span CancellationSpan,
	nextID *uint64,
) []*primitive.Value {
	sharedID := *nextID
	*nextID++

	shared := primitive.NewValue()
	for k := 0; k < span.Length; k++ {
		shared.SetTokenID(k, left.TokenID(span.LeftStart+k))
	}
	shared.SetValueID(sharedID)
	shared.SetPrevValueID(left.ValueID())
	shared[primitive.StateSlotIndex] = 1

	result := []*primitive.Value{shared}

	if rem := extractRemainder(left, span.LeftStart, span.Length, sharedID, nextID); rem != nil {
		result = append(result, rem)
	}
	if rem := extractRemainder(right, span.RightStart, span.Length, sharedID, nextID); rem != nil {
		result = append(result, rem)
	}

	errnie.Trace(
		"compute.kernel.cpu.trie.buildEmittedValues",
		"shared", shared.TokenIDs(),
		"left", left.TokenIDs(),
		"right", right.TokenIDs(),
		"span", span,
		"sharedID", sharedID,
		"numEmitted", len(result),
	)

	return result
}

func extractRemainder(
	src *primitive.Value,
	spanStart, spanLength int,
	sharedID uint64,
	nextID *uint64,
) *primitive.Value {
	rem := primitive.NewValue()
	slot := 0
	for i := 0; i < primitive.Region0TokenCount; i++ {
		if i >= spanStart && i < spanStart+spanLength {
			continue
		}
		tok := src.TokenID(i)
		if tok == 0 {
			continue
		}
		rem.SetTokenID(slot, tok)
		slot++
	}
	if slot == 0 {
		return nil
	}

	id := *nextID
	*nextID++
	rem.SetValueID(id)
	rem.SetNextValueID(sharedID)
	rem.SetPrevValueID(src.ValueID())
	rem[primitive.StateSlotIndex] = 1
	return rem
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

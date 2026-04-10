package episodic

import (
	"fmt"
	"math"
	"strings"

	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/numeric/probability"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Event is one labeled token sequence stored in the rolling buffer.
*/
type Event struct {
	ID       string
	Tokens   []string
	Label    string
	Step     int
	Geometry primitive.FrameMultivector
}

/*
CoordinateResolver resolves an episodic event to the current manifold
coordinate in the backing store.
*/
type CoordinateResolver func(Event) (primitive.FrameMultivector, bool)

/*
Episode is the public snapshot of one buffer row.
*/
type Episode struct {
	ID        string
	Tokens    []string
	Label     string
	Timestamp int
}

/*
Buffer is a fixed-capacity ring of labeled token sequences that produces
recency-weighted next-token distributions for a given context suffix.
*/
type Buffer struct {
	events          []Event
	capacity        int
	sequenceCounter uint64
	alpha           float64
	neighborLimit   int
	recencyWeight   float64
	decayGamma      float64
}

/*
NewBuffer allocates an episodic ring buffer.
*/
func NewBuffer(capacity int, alpha float64, neighborLimit int, recencyWeight float64, decayGamma float64) *Buffer {
	if capacity <= 0 {
		return nil
	}

	return &Buffer{
		events:        make([]Event, 0, capacity),
		capacity:      capacity,
		alpha:         alpha,
		neighborLimit: neighborLimit,
		recencyWeight: recencyWeight,
		decayGamma:    decayGamma,
	}
}

/*
Push appends a labeled token sequence, evicting the oldest when full.
*/
func (buffer *Buffer) Push(tokens []string, label string, step int) {
	if buffer == nil {
		return
	}

	buffer.sequenceCounter++

	buffer.events = append(buffer.events, Event{
		ID:       fmt.Sprintf("ep_%d", buffer.sequenceCounter),
		Tokens:   append([]string(nil), tokens...),
		Label:    label,
		Step:     step,
		Geometry: eventGeometry(tokens),
	})

	overflow := len(buffer.events) - buffer.capacity

	if overflow > 0 {
		buffer.events = buffer.events[overflow:]
	}
}

/*
Empty reports whether the buffer has no events.
*/
func (buffer *Buffer) Empty() bool {
	return buffer == nil || len(buffer.events) == 0
}

/*
Snapshot returns a copy of the buffer contents.
*/
func (buffer *Buffer) Snapshot(contentFilter func([]string) []string) []Episode {
	if buffer.Empty() {
		return nil
	}

	out := make([]Episode, 0, len(buffer.events))

	for _, event := range buffer.events {
		tokens := event.Tokens

		if contentFilter != nil {
			tokens = contentFilter(event.Tokens)
		}

		out = append(out, Episode{
			ID:        event.ID,
			Tokens:    append([]string(nil), tokens...),
			Label:     event.Label,
			Timestamp: event.Step,
		})
	}

	return out
}

/*
NextDistribution produces a recency-weighted next-token probability map
from buffer events whose suffix matches contextTokens.
*/
func (buffer *Buffer) NextDistribution(contextTokens []string, label string) map[string]float64 {
	if buffer.Empty() {
		return nil
	}

	limit := buffer.neighborLimit

	if limit <= 0 {
		limit = 5
	}

	counts := make(map[string]float64)
	matches := 0
	bufferLength := len(buffer.events)

	for index := bufferLength - 1; index >= 0 && matches < limit; index-- {
		event := buffer.events[index]

		if label != "" && event.Label != label {
			continue
		}

		nextToken, found := nextTokenAfterContext(event.Tokens, contextTokens)

		if !found {
			continue
		}

		recency := 1.0

		if buffer.decayGamma > 0 && buffer.decayGamma < 1 {
			recency = math.Pow(buffer.decayGamma, float64(matches))
		} else if buffer.recencyWeight > 0 && bufferLength > 0 {
			recency += float64(index) / float64(bufferLength) * buffer.recencyWeight
		}

		counts[nextToken] += recency
		matches++
	}

	probability.NormalizeMap(counts)

	return counts
}

/*
Blend merges an episodic next-token distribution into a trie distribution
using the configured alpha. Returns the blended map.
*/
func (buffer *Buffer) Blend(contextTokens []string, label string, trie map[string]float64, alpha float64) map[string]float64 {
	if buffer.Empty() {
		return trie
	}

	episodic := buffer.NextDistribution(contextTokens, label)

	if len(episodic) == 0 {
		return trie
	}

	if alpha <= 0 {
		return trie
	}

	if len(trie) == 0 {
		return episodic
	}

	merged := make(map[string]float64)

	for token, prob := range trie {
		merged[token] = (1 - alpha) * prob
	}

	for token, prob := range episodic {
		merged[token] += alpha * prob
	}

	probability.NormalizeMap(merged)

	return merged
}

/*
Realign rotates episodic coordinates toward current backing-store coordinates.
Callers should use this from an idle consolidation loop after the main trie has
moved; the buffer only owns the in-place Procrustes update.
*/
func (buffer *Buffer) Realign(resolve CoordinateResolver, sampleLimit int) error {
	if buffer.Empty() || resolve == nil {
		return nil
	}

	if sampleLimit <= 0 || sampleLimit > len(buffer.events) {
		sampleLimit = len(buffer.events)
	}

	matA := make([][]float64, 0, sampleLimit)
	matB := make([][]float64, 0, sampleLimit)

	for eventIndex := 0; eventIndex < sampleLimit; eventIndex++ {
		event := buffer.events[eventIndex]

		if event.Geometry.IsZero() {
			continue
		}

		current, ok := resolve(event)

		if !ok || current.IsZero() {
			continue
		}

		matA = append(matA, eventGeometryRow(event.Geometry))
		matB = append(matB, eventGeometryRow(current))
	}

	if len(matA) < primitive.RegionWords {
		return nil
	}

	result, err := geometry.Procrustes(
		matA,
		matB,
		len(matA),
		primitive.RegionWords,
	)

	if err != nil {
		return err
	}

	for eventIndex := range buffer.events {
		buffer.events[eventIndex].Geometry = rotateEventGeometry(
			result.R,
			buffer.events[eventIndex].Geometry,
		)
	}

	return nil
}

/*
PickRandom returns a random event from the buffer using the provided index.
*/
func (buffer *Buffer) PickRandom(index int) *Event {
	if buffer.Empty() {
		return nil
	}

	event := buffer.events[index%len(buffer.events)]

	return &event
}

/*
Len returns the number of events in the buffer.
*/
func (buffer *Buffer) Len() int {
	if buffer == nil {
		return 0
	}

	return len(buffer.events)
}

func eventGeometry(tokens []string) primitive.FrameMultivector {
	return primitive.NewFrameMultivector([]byte(strings.Join(tokens, "\x00")))
}

func eventGeometryRow(vector primitive.FrameMultivector) []float64 {
	row := make([]float64, primitive.RegionWords)

	for idx := range primitive.RegionWords {
		row[idx] = vector[idx]
	}

	return row
}

func rotateEventGeometry(
	rotation [][]float64,
	vector primitive.FrameMultivector,
) primitive.FrameMultivector {
	if len(rotation) != primitive.RegionWords || vector.IsZero() {
		return vector
	}

	var out primitive.FrameMultivector

	for row := range primitive.RegionWords {
		if len(rotation[row]) != primitive.RegionWords {
			return vector
		}

		for col := range primitive.RegionWords {
			out[row] += rotation[row][col] * vector[col]
		}
	}

	return out.Normalize()
}

func nextTokenAfterContext(sequence []string, context []string) (string, bool) {
	if len(sequence) == 0 {
		return "", false
	}

	if len(context) == 0 {
		return sequence[0], true
	}

	for start := 0; start <= len(sequence)-len(context)-1; start++ {
		matched := true

		for contextIndex := range context {
			if sequence[start+contextIndex] != context[contextIndex] {
				matched = false

				break
			}
		}

		if !matched {
			continue
		}

		nextIndex := start + len(context)

		if nextIndex >= len(sequence) {
			return "", false
		}

		return sequence[nextIndex], true
	}

	return "", false
}

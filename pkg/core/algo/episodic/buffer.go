package episodic

import (
	"fmt"
	"math"

	"github.com/theapemachine/six/pkg/core/numeric/probability"
)

/*
Event is one labeled token sequence stored in the rolling buffer.
*/
type Event struct {
	ID     string
	Tokens []string
	Label  string
	Step   int
}

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
		ID:     fmt.Sprintf("ep_%d", buffer.sequenceCounter),
		Tokens: append([]string(nil), tokens...),
		Label:  label,
		Step:   step,
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

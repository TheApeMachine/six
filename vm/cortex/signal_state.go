package cortex

import (
	"sort"

	"github.com/theapemachine/six/data"
)

/*
signalKey is the identity of a routed computational signal inside one node.
*/
type signalKey struct {
	Chord data.Chord
	Mask  data.Chord
	Face  int
}

func newSignalKey(tok Token) signalKey {
	return signalKey{
		Chord: tok.Chord,
		Mask:  tok.SignalMask,
		Face:  tok.LogicalFace,
	}
}

/*
rememberSignal records the strongest observation of a signal and suppresses
weaker echoes. Returns true when the signal is novel enough to process.
*/
func (node *Node) rememberSignal(tok Token) bool {
	key := newSignalKey(tok)

	if ttl, exists := node.signalTTL[key]; exists {
		node.signalSupport[key]++

		if ttl >= tok.TTL {
			return false
		}

		node.signalTTL[key] = tok.TTL
		node.Signals[node.signalIndex[key]] = tok
		return true
	}

	node.signalIndex[key] = len(node.Signals)
	node.signalTTL[key] = tok.TTL
	node.signalSupport[key] = 1
	node.Signals = append(node.Signals, tok)

	return true
}

type signalChordScore struct {
	Chord  data.Chord
	Score  int
	Active int
}

/*
extractResults condenses the sink's routed signals into a ranked list of
candidate reasoning residues.
*/
func (graph *Graph) extractResults() []data.Chord {
	if graph.sink == nil {
		return nil
	}

	chordScores := make(map[data.Chord]int)

	for _, sig := range graph.sink.Signals {
		if sig.Chord.ActiveCount() == 1 && sig.Chord.Has(256) {
			continue
		}

		key := newSignalKey(sig)
		support := graph.sink.signalSupport[key]

		if support == 0 {
			support = 1
		}

		score := support * max(sig.Chord.ActiveCount(), 1)
		chordScores[sig.Chord] += score
	}

	for _, tool := range graph.ToolNodes() {
		if tool.Payload.ActiveCount() == 0 {
			continue
		}

		score := max(tool.Support, 1) * max(tool.Payload.ActiveCount(), 1)
		chordScores[tool.Payload] += score
	}

	ranked := make([]signalChordScore, 0, len(chordScores))

	for chord, score := range chordScores {
		if chord.ActiveCount() == 0 {
			continue
		}

		ranked = append(ranked, signalChordScore{
			Chord:  chord,
			Score:  score,
			Active: chord.ActiveCount(),
		})
	}

	sort.Slice(ranked, func(left, right int) bool {
		if ranked[left].Score == ranked[right].Score {
			return ranked[left].Active > ranked[right].Active
		}

		return ranked[left].Score > ranked[right].Score
	})

	if len(ranked) > 16 {
		ranked = ranked[:16]
	}

	results := make([]data.Chord, len(ranked))

	for idx, entry := range ranked {
		results[idx] = entry.Chord
	}

	return results
}

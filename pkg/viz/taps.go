package viz

import (
	"fmt"
	"strconv"
)

/*
Taps are bridge functions that translate domain-specific data into viz Events.
They are called from a thin instrumentation layer (viz/instrument.go) that
wraps system components. The core system never imports viz — instrumentation
is injected from the outside (cmd layer) via functional options or observers.

This file provides helper constructors for common event patterns. The actual
hook-up happens in cmd/viz.go where we wire observers and callbacks.
*/

// --- Kadabra / DHT events ---

func NodeCreated(nodeID uint64, label string) Event {
	ev := NewEvent(EventNodeCreated, fmtNodeID(nodeID))
	ev.Label = label
	return ev
}

func NodeUpdated(nodeID uint64, vals map[string]float64) Event {
	ev := NewEvent(EventNodeUpdated, fmtNodeID(nodeID))
	ev.Values = vals
	return ev
}

func PeerAdded(from, to uint64, bucket int) Event {
	ev := NewEvent(EventPeerAdded, fmtNodeID(from))
	ev.Target = fmtNodeID(to)
	ev.Values = map[string]float64{"bucket": float64(bucket)}
	return ev
}

func PeerLatency(from, to uint64, latencyMs float64) Event {
	ev := NewEvent(EventPeerLatency, fmtNodeID(from))
	ev.Target = fmtNodeID(to)
	ev.Values = map[string]float64{"latency_ms": latencyMs}
	return ev
}

func ValuePublished(nodeID uint64, key uint64, label string) Event {
	ev := NewEvent(EventValuePublished, fmtNodeID(nodeID))
	ev.Label = label
	ev.Meta = map[string]string{"key": strconv.FormatUint(key, 16)}
	return ev
}

func ValueReplicated(from, to uint64, key uint64) Event {
	ev := NewEvent(EventValueReplicated, fmtNodeID(from))
	ev.Target = fmtNodeID(to)
	ev.Meta = map[string]string{"key": strconv.FormatUint(key, 16)}
	return ev
}

// --- Gossip & Field ---

func GossipSent(from uint64, epoch uint64) Event {
	ev := NewEvent(EventGossipSent, fmtNodeID(from))
	ev.Values = map[string]float64{"epoch": float64(epoch)}
	return ev
}

func GossipReceived(to uint64, from uint64, epoch uint64) Event {
	ev := NewEvent(EventGossipReceived, fmtNodeID(to))
	ev.Target = fmtNodeID(from)
	ev.Values = map[string]float64{"epoch": float64(epoch)}
	return ev
}

func FieldDigestEvent(nodeID uint64, surprisal, entropy, growth float64) Event {
	ev := NewEvent(EventFieldDigest, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"surprisal": surprisal,
		"entropy":   entropy,
		"growth":    growth,
	}
	return ev
}

func EigenmodeDetected(nodeID uint64, modeCount int, dominantEnergy float64) Event {
	ev := NewEvent(EventEigenmodeDetected, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"mode_count":      float64(modeCount),
		"dominant_energy": dominantEnergy,
	}
	return ev
}

func FieldPressureEvent(nodeID uint64, decay, learning, prune float64) Event {
	ev := NewEvent(EventFieldPressure, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"decay":    decay,
		"learning": learning,
		"prune":    prune,
	}
	return ev
}

// --- MarkovTrie ---

func TrieInsertEvent(nodeID uint64, sequence, label string) Event {
	ev := NewEvent(EventTrieInsert, fmtNodeID(nodeID))
	ev.Label = label
	// Large enough for typical tokenizer/code rows in paper runs; JSON still small vs timeline cap.
	ev.Meta = map[string]string{"sequence": truncate(sequence, 512)}
	return ev
}

func TriePredictEvent(nodeID uint64, label string, confidence float64) Event {
	ev := NewEvent(EventTriePredict, fmtNodeID(nodeID))
	ev.Label = label
	ev.Values = map[string]float64{"confidence": confidence}
	return ev
}

func TrieClassifyEvent(nodeID uint64, scores map[string]float64) Event {
	ev := NewEvent(EventTrieClassify, fmtNodeID(nodeID))
	ev.Values = scores
	return ev
}

func TrieExperienceEvent(nodeID uint64, surprisal float64, label string) Event {
	ev := NewEvent(EventTrieExperience, fmtNodeID(nodeID))
	ev.Label = label
	ev.Values = map[string]float64{"surprisal": surprisal}
	return ev
}

func AdaptiveUpdateEvent(nodeID uint64, vals map[string]float64) Event {
	ev := NewEvent(EventAdaptiveUpdate, fmtNodeID(nodeID))
	ev.Values = vals
	return ev
}

// --- Intra-node field (trie-to-trie within a single node) ---

func TrieCouplingEvent(nodeID uint64, trieA, trieB int, coupling float64) Event {
	ev := NewEvent(EventTrieCoupling, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"trie_a":   float64(trieA),
		"trie_b":   float64(trieB),
		"coupling": coupling,
	}

	return ev
}

func TrieModeEvent(nodeID uint64, trieIdx int, modeIdx int, aligned bool, energy float64) Event {
	ev := NewEvent(EventTrieMode, fmtNodeID(nodeID))

	alignedVal := 0.0
	if aligned {
		alignedVal = 1.0
	}

	ev.Values = map[string]float64{
		"trie_idx": float64(trieIdx),
		"mode_idx": float64(modeIdx),
		"aligned":  alignedVal,
		"energy":   energy,
	}

	return ev
}

func TriePressureEvent(nodeID uint64, trieIdx int, decay, learn, decayMul, learnMul float64) Event {
	ev := NewEvent(EventTriePressure, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"trie_idx":  float64(trieIdx),
		"decay":     decay,
		"learn":     learn,
		"decay_mul": decayMul,
		"learn_mul": learnMul,
	}

	return ev
}

func TrieSignalEvent(nodeID uint64, trieIdx int, surprisal, entropy, growth float64) Event {
	ev := NewEvent(EventTrieSignal, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"trie_idx":  float64(trieIdx),
		"surprisal": surprisal,
		"entropy":   entropy,
		"growth":    growth,
	}

	return ev
}

// --- Hierarchical Beam Search ---

/*
BeamCollectEvent fires when the node collects continuations from its
tries before feeding them to the node-level beam. Shows how many tries
contributed and total candidate count.
*/
func BeamCollectEvent(nodeID uint64, trieCount, continuationCount int) Event {
	ev := NewEvent(EventBeamCollect, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"trie_count":         float64(trieCount),
		"continuation_count": float64(continuationCount),
	}

	return ev
}

/*
BeamComposeEvent fires after the node-level beam selects winners from
the collected trie continuations. Shows how many survived and the
best score.
*/
func BeamComposeEvent(nodeID uint64, selectedCount, rejectedCount int, bestScore float64) Event {
	ev := NewEvent(EventBeamCompose, fmtNodeID(nodeID))
	ev.Values = map[string]float64{
		"selected_count": float64(selectedCount),
		"rejected_count": float64(rejectedCount),
		"best_score":     bestScore,
	}

	return ev
}

/*
BeamBreakEvent fires when the node sends a BreakBeam signal to a
specific trie, resetting its beam so it can re-search.
*/
func BeamBreakEvent(nodeID uint64, trieID uint64) Event {
	ev := NewEvent(EventBeamBreak, fmtNodeID(nodeID))
	ev.Target = fmt.Sprintf("trie_%x", trieID)
	ev.Values = map[string]float64{
		"trie_id": float64(trieID),
	}

	return ev
}

/*
BeamConvergeEvent fires when the node-level beam produces its final
output for a prompt.
*/
func BeamConvergeEvent(nodeID uint64, sequence string, score float64) Event {
	ev := NewEvent(EventBeamConverge, fmtNodeID(nodeID))
	ev.Label = truncate(sequence, 128)
	ev.Values = map[string]float64{
		"score": score,
	}

	return ev
}

// --- Compute Pool ---

func PoolScheduleEvent(action string, queueSize, workers int) Event {
	ev := NewEvent(EventPoolSchedule, "pool")
	ev.Label = action
	ev.Values = map[string]float64{
		"queue_size": float64(queueSize),
		"workers":    float64(workers),
	}
	return ev
}

func PoolCompleteEvent(action string, durationMs int) Event {
	ev := NewEvent(EventPoolComplete, "pool")
	ev.Label = action
	ev.Values = map[string]float64{"duration_ms": float64(durationMs)}
	return ev
}

// --- Prompt ---

func PromptEvent(prompt string) Event {
	ev := NewEvent(EventPrompt, "user")
	ev.Meta = map[string]string{"prompt": prompt}
	return ev
}

func PromptResultEvent(generation string, scores map[string]float64) Event {
	ev := NewEvent(EventPromptResult, "system")
	ev.Meta = map[string]string{"generation": truncate(generation, 256)}
	ev.Values = scores
	return ev
}

func fmtNodeID(id uint64) string {
	return fmt.Sprintf("node_%x", id)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

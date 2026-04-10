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

func TrieInsertEvent(nodeID uint64, trieIdx int, sequence, label string) Event {
	ev := NewEvent(EventTrieInsert, fmtNodeID(nodeID))
	ev.Label = label

	if trieIdx >= 0 {
		ev.Values["trie_idx"] = float64(trieIdx)
	}

	// Large enough for tokenizer/code rows in paper runs; JSON stays small vs timeline cap.
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

/*
CompilerCompileEvent records a deferred-layout compile before ALU dispatch.
targetLabel is cpu|metal|cuda; operation is the Intent opcode constant.
*/
func CompilerCompileEvent(
	targetLabel string,
	operation uint64,
	correlation uint64,
	compileNanos int64,
	batchAffinity bool,
	finalizerDepth int,
) Event {
	ev := NewEvent(EventCompilerCompile, "compute")
	ev.Label = targetLabel

	batchVal := 0.0

	if batchAffinity {
		batchVal = 1.0
	}

	ev.Values = map[string]float64{
		"operation":       float64(operation),
		"compile_ns":      float64(compileNanos),
		"batch_affinity":  batchVal,
		"finalizer_depth": float64(finalizerDepth),
	}

	if correlation != 0 {
		ev.Meta["correlation"] = strconv.FormatUint(correlation, 10)
	}

	return ev
}

/*
ALUDispatchEvent records substrate execution after the frame opcode is fixed.
durationMs matches PoolComplete for the same dispatch when both fire.
*/
func ALUDispatchEvent(substrateName string, opcode uint8, correlation uint64, durationMs int) Event {
	ev := NewEvent(EventALUDispatch, "compute")
	ev.Label = substrateName
	ev.Values = map[string]float64{
		"opcode":      float64(opcode),
		"duration_ms": float64(durationMs),
	}

	if correlation != 0 {
		ev.Meta["correlation"] = strconv.FormatUint(correlation, 10)
	}

	return ev
}

/*
FinalizerRunEvent fires after the compiler finalizer chain (post-execute).
*/
func FinalizerRunEvent(correlation uint64, depth int, emitted int, hadError bool) Event {
	ev := NewEvent(EventFinalizerRun, "compute")
	ev.Label = "finalize"

	errVal := 0.0

	if hadError {
		errVal = 1.0
	}

	ev.Values = map[string]float64{
		"depth":   float64(depth),
		"emitted": float64(emitted),
		"error":   errVal,
	}

	if correlation != 0 {
		ev.Meta["correlation"] = strconv.FormatUint(correlation, 10)
	}

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

/*
TrieGraphSnapshotEvent ships a JSON graph payload for one markovtrie.Store column.
The browser lays out vertices and edges to match the live trie (possibly truncated).
*/
func TrieGraphSnapshotEvent(nodeID uint64, trieIdx int, graphJSON []byte) Event {
	ev := NewEvent(EventTrieGraphSnapshot, fmtNodeID(nodeID))
	ev.Values["trie_idx"] = float64(trieIdx)
	ev.Meta["graph"] = string(graphJSON)

	return ev
}

// --- Dataset / Tokenizer pipeline ---

/*
DatasetReadEvent fires when the dataset emits a chunk of raw bytes.
bytesRead is the number of bytes in this chunk; totalBytes is the
running total for this ingest session.
*/
func DatasetReadEvent(datasetName string, bytesRead, totalBytes int64, label string) Event {
	ev := NewEvent(EventDatasetRead, "dataset")
	ev.Label = label
	ev.Values = map[string]float64{
		"bytes_read":  float64(bytesRead),
		"total_bytes": float64(totalBytes),
	}
	ev.Meta = map[string]string{"dataset": datasetName}
	return ev
}

/*
TokenizerChunkEvent fires when the tokenizer writes raw bytes into its
ring buffer (ingest side).
*/
func TokenizerChunkEvent(bytesWritten int) Event {
	ev := NewEvent(EventTokenizerChunk, "tokenizer")
	ev.Values = map[string]float64{
		"bytes_written": float64(bytesWritten),
	}
	return ev
}

/*
TokenizerEmitEvent fires when the tokenizer mints a new Value from a
drained chunk and publishes it downstream.
*/
func TokenizerEmitEvent(valueID uint64, label string) Event {
	ev := NewEvent(EventTokenizerEmit, "tokenizer")
	ev.Label = label
	ev.Meta = map[string]string{"value_id": strconv.FormatUint(valueID, 16)}
	return ev
}

/*
QueueSubmitEvent fires when the pool queue accepts a new work item.
*/
func QueueSubmitEvent(inflight int64) Event {
	ev := NewEvent(EventQueueSubmit, "queue")
	ev.Values = map[string]float64{
		"inflight": float64(inflight),
	}
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

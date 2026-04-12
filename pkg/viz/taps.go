package viz

import (
	"fmt"
	"strconv"
	"strings"
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
	applyVizLayout(&ev, "node", vizBandDHT, fmt.Sprintf("node_created|%s|%s", fmtNodeID(nodeID), label))
	return ev
}

func NodeUpdated(nodeID uint64, vals map[string]float64) Event {
	ev := NewEvent(EventNodeUpdated, fmtNodeID(nodeID))
	ev.Values = vals
	applyVizLayout(&ev, "node", vizBandDHT, fmt.Sprintf("node_updated|%s", fmtNodeID(nodeID)))
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
	applyVizLayout(&ev, "trie_insert", vizBandTrie, fmt.Sprintf("%s|%d|%s", fmtNodeID(nodeID), trieIdx, label))

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
	applyVizLayout(&ev, "beam_collect", vizBandBeam, fmt.Sprintf("%s|%d", fmtNodeID(nodeID), trieCount))

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
	applyVizLayout(&ev, "beam_compose", vizBandBeam, fmt.Sprintf("%s|%d", fmtNodeID(nodeID), selectedCount))

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
	applyVizLayout(&ev, "beam_break", vizBandBeam, fmt.Sprintf("%s|%x", fmtNodeID(nodeID), trieID))

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
	applyVizLayout(&ev, "beam_converge", vizBandBeam, fmt.Sprintf("%s|%0.4f", fmtNodeID(nodeID), score))

	return ev
}

// --- Compute Pool ---

func PoolScheduleEvent(action string, queueSize, workers int, valueID uint64) Event {
	ev := NewEvent(EventPoolSchedule, "pool")
	ev.Label = action
	ev.Values = map[string]float64{
		"queue_size": float64(queueSize),
		"workers":    float64(workers),
	}
	ev.Meta = map[string]string{"value_id": strconv.FormatUint(valueID, 16)}
	applyVizLayout(&ev, "pool_schedule", vizBandPool, fmt.Sprintf("%s|%s", action, strconv.FormatUint(valueID, 16)))
	return ev
}

func PoolCompleteEvent(action string, durationMs int, valueID uint64) Event {
	ev := NewEvent(EventPoolComplete, "pool")
	ev.Label = action
	ev.Values = map[string]float64{"duration_ms": float64(durationMs)}
	ev.Meta = map[string]string{"value_id": strconv.FormatUint(valueID, 16)}
	applyVizLayout(&ev, "pool_complete", vizBandPool, fmt.Sprintf("%s|%s|%d", action, strconv.FormatUint(valueID, 16), durationMs))
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

	applyVizLayout(&ev, "compile", vizBandCompute, fmt.Sprintf("%s|%d|%d", targetLabel, operation, compileNanos))

	return ev
}

/*
ALUDispatchEvent records substrate execution after the frame opcode is fixed.
durationMs matches PoolComplete for the same dispatch when both fire.
*/
func ALUDispatchEvent(substrateName string, opcode uint8, correlation uint64, durationMs int, valueID uint64) Event {
	ev := NewEvent(EventALUDispatch, "compute")
	ev.Label = substrateName
	ev.Values = map[string]float64{
		"opcode":      float64(opcode),
		"duration_ms": float64(durationMs),
	}

	if correlation != 0 {
		ev.Meta["correlation"] = strconv.FormatUint(correlation, 10)
	}
	ev.Meta["value_id"] = strconv.FormatUint(valueID, 16)

	applyVizLayout(&ev, "alu", vizBandCompute, fmt.Sprintf("%s|%d|%s", substrateName, opcode, strconv.FormatUint(valueID, 16)))

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

	applyVizLayout(&ev, "finalizer", vizBandCompute, fmt.Sprintf("%d|%d|%d", depth, emitted, correlation))

	return ev
}

// --- Prompt ---

func PromptEvent(prompt string) Event {
	ev := NewEvent(EventPrompt, "user")
	ev.Meta = map[string]string{"prompt": prompt}
	applyVizLayout(&ev, "prompt", vizBandPrompt, prompt)
	return ev
}

func PromptResultEvent(generation string, scores map[string]float64) Event {
	ev := NewEvent(EventPromptResult, "system")
	ev.Meta = map[string]string{"generation": truncate(generation, 256)}
	ev.Values = scores
	applyVizLayout(&ev, "prompt_result", vizBandPrompt, truncate(generation, 64))
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
	applyVizLayout(&ev, "trie_graph", vizBandTrie, fmt.Sprintf("%s|%d|%d", fmtNodeID(nodeID), trieIdx, len(graphJSON)))

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
	applyVizLayout(&ev, "dataset", vizBandIngest, fmt.Sprintf("%s|%s|%d", datasetName, label, bytesRead))
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
	applyVizLayout(&ev, "tokenizer_chunk", vizBandIngest, strconv.Itoa(bytesWritten))
	return ev
}

/*
TokenizerEmitEvent fires when the tokenizer mints a new Value from a
drained chunk and publishes it downstream.
*/
func TokenizerEmitEvent(valueID uint64, label string, tokenContent string) Event {
	ev := NewEvent(EventTokenizerEmit, "tokenizer")
	ev.Label = label
	vidHex := strconv.FormatUint(valueID, 16)
	ev.Meta = map[string]string{
		"value_id": vidHex,
		"content":  truncate(tokenContent, 128),
	}
	applyVizLayout(&ev, "tokenizer_emit", vizBandTokenizerVal, fmt.Sprintf("%s|%s", vidHex, label))
	return ev
}

/*
QueueSubmitEvent fires when the pool queue accepts a new work item.
*/
func QueueSubmitEvent(inflight int64, valueID, prevID, nextID uint64, content string) Event {
	ev := NewEvent(EventQueueSubmit, "queue")
	ev.Label = truncate(content, 128)
	ev.Values["inflight"] = float64(inflight)
	ev.Meta["value_id"] = strconv.FormatUint(valueID, 16)
	ev.Meta["prev_id"] = strconv.FormatUint(prevID, 16)
	ev.Meta["next_id"] = strconv.FormatUint(nextID, 16)
	applyVizLayoutQueue(&ev, inflight, valueID)
	return ev
}

func CausalHubProbeEvent(valueID uint64, prefixStart, prefixWords int, mask uint64, depth uint64, status string) Event {
	ev := NewEvent(EventCausalHubProbe, "queue")
	ev.Label = "causal_hub"
	ev.Values = map[string]float64{
		"prefix_start": float64(prefixStart),
		"prefix_words": float64(prefixWords),
		"depth":        float64(depth),
	}
	ev.Meta = map[string]string{
		"value_id": strconv.FormatUint(valueID, 16),
		"mask":     strconv.FormatUint(mask, 16),
		"status":   status,
	}
	applyVizLayout(&ev, "causal_hub", vizBandQueue, fmt.Sprintf("%s|%d|%s", strconv.FormatUint(valueID, 16), depth, status))
	return ev
}

func HolographicCrossoverEvent(valueID uint64) Event {
	ev := NewEvent(EventHolographicCrossover, "queue")
	applyVizLayout(&ev, "crossover", vizBandQueue, strconv.FormatUint(valueID, 16))
	return ev
}

func SenseEvent(valueID uint64, amplitude, index int) Event {
	ev := NewEvent(EventSense, "queue")
	applyVizLayout(&ev, "sense", vizBandQueue, fmt.Sprintf("%s|%d|%d", strconv.FormatUint(valueID, 16), amplitude, index))
	return ev
}

// --- Orchestrator / Community Routing ---

func CommunityCreatedEvent(communityID int, initialAffinity []uint64) Event {
	ev := NewEvent(EventCommunityCreated, "orchestrator")
	ev.Values = map[string]float64{"community_id": float64(communityID)}

	if len(initialAffinity) > 0 {
		var builder strings.Builder

		builder.Grow(len(initialAffinity) * 16)

		for _, word := range initialAffinity {
			fmt.Fprintf(&builder, "%016x", word)
		}

		ev.Meta["initial_affinity"] = builder.String()
	}

	applyVizLayoutCommunity(&ev, communityID, "create")

	return ev
}

func ValueJoinedCommunityEvent(valueID uint64, communityID int, distance int) Event {
	ev := NewEvent(EventValueJoinedCommunity, "orchestrator")
	ev.Values = map[string]float64{
		"community_id": float64(communityID),
		"distance":     float64(distance),
	}
	ev.Meta = map[string]string{"value_id": strconv.FormatUint(valueID, 16)}
	applyVizLayoutCommunity(&ev, communityID, fmt.Sprintf("join|%s", strconv.FormatUint(valueID, 16)))
	return ev
}

func CommunitySaturatedEvent(communityID int, saturation float64) Event {
	ev := NewEvent(EventCommunitySaturated, "orchestrator")
	ev.Values = map[string]float64{
		"community_id": float64(communityID),
		"saturation":   saturation,
	}
	applyVizLayoutCommunity(&ev, communityID, fmt.Sprintf("sat|%0.3f", saturation))
	return ev
}

func CommunityActionEvent(communityID int, actionID uint64, program string, resonance float64) Event {
	ev := NewEvent(EventCommunityAction, "orchestrator")
	ev.Label = program
	ev.Values = map[string]float64{
		"community_id": float64(communityID),
		"resonance":    resonance,
	}
	idHex := strconv.FormatUint(actionID, 16)
	ev.Meta = map[string]string{"action_id": idHex}
	applyVizLayoutProgram(&ev, communityID, program, idHex)
	return ev
}

func CommunityReactionEvent(communityID int, reactionID uint64, program string) Event {
	ev := NewEvent(EventCommunityReaction, "orchestrator")
	ev.Label = program
	ev.Values = map[string]float64{"community_id": float64(communityID)}
	idHex := strconv.FormatUint(reactionID, 16)
	ev.Meta = map[string]string{"reaction_id": idHex}
	applyVizLayoutProgram(&ev, communityID, program, idHex)
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

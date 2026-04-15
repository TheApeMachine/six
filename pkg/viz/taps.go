package viz

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Taps are bridge functions that translate domain-specific data into viz Events.
They are called from a thin instrumentation layer (viz/instrument.go) that
wraps system components. The core system never imports viz — instrumentation
is injected from the outside (cmd layer) via functional options or observers.

This file provides helper constructors for common event patterns. The actual
hook-up happens in cmd/viz.go where we wire observers and callbacks.
*/

// --- Node / routing visualization ---

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

// --- Sequence / beam viz (legacy names: Trie*) ---

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
		"inflight":   float64(queueSize),
		"queue_size": float64(queueSize),
		"workers":    float64(workers),
	}
	ev.Meta = map[string]string{"value_id": FormatValueIDHex(valueID)}
	applyVizLayout(&ev, "pool_schedule", vizBandPool, fmt.Sprintf("%s|%s", action, FormatValueIDHex(valueID)))
	return ev
}

func PoolCompleteEvent(action string, durationNanos int64, valueID uint64) Event {
	ev := NewEvent(EventPoolComplete, "pool")
	ev.Label = action
	ev.Values = map[string]float64{
		"duration_ms": float64(durationNanos) / 1_000_000.0,
		"duration_ns": float64(durationNanos),
	}
	ev.Meta = map[string]string{"value_id": FormatValueIDHex(valueID)}
	applyVizLayout(&ev, "pool_complete", vizBandPool, fmt.Sprintf("%s|%s|%d", action, FormatValueIDHex(valueID), durationNanos))
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
durationNanos matches PoolComplete for the same dispatch when both fire.
*/
func ALUDispatchEvent(substrateName string, opcode uint8, correlation uint64, durationNanos int64, valueID uint64) Event {
	ev := NewEvent(EventALUDispatch, "compute")
	ev.Label = substrateName
	ev.Values = map[string]float64{
		"opcode":      float64(opcode),
		"duration_ms": float64(durationNanos) / 1_000_000.0,
		"duration_ns": float64(durationNanos),
	}
	ev.Meta["substrate"] = substrateName

	if correlation != 0 {
		ev.Meta["correlation"] = strconv.FormatUint(correlation, 10)
	}
	ev.Meta["value_id"] = FormatValueIDHex(valueID)

	applyVizLayout(&ev, "alu", vizBandCompute, fmt.Sprintf("%s|%d|%s", substrateName, opcode, FormatValueIDHex(valueID)))

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
TrieGraphSnapshotEvent ships a JSON graph payload for debugging Value linkage /
sequence layout in the browser (possibly truncated).
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
TokenizerEmitEvent fires after InstallProgram has stamped the affinity firmware
on the Value. Layout words (affinity, chain, properties) are not duplicated in
JSON — the viz server also ships WireFrameValue with the full Value.Bytes image.
*/
func TokenizerEmitEvent(seg *primitive.Value, label string) Event {
	ev := NewEvent(EventTokenizerEmit, "tokenizer")

	if seg == nil {
		return ev
	}

	ev.Label = label
	vidHex := FormatValueIDHex(seg.ID())
	tokenContent := truncate(seg.String(), 128)

	ev.Meta = map[string]string{
		"value_id": vidHex,
		"content":  tokenContent,
		"program":  "affinity",
	}

	applyVizLayout(&ev, "tokenizer_emit", vizBandTokenizerVal, fmt.Sprintf("%s|%s", vidHex, label))

	return ev
}

/*
queueSubmitChainMeta mirrors the frontend chain resolution: committed prev/next
words first, else the first two asset words where the orchestrator stages ids.
Same memory as WireFrameValue at this instant — included so JSON consumers see
links even when a binary frame is short or reordered on the socket.
*/
func queueSubmitChainMeta(ev *Event, value *primitive.Value) {
	if value == nil {
		return
	}

	assetStart, _ := primitive.AssetRegion.WordExtent()
	prevStart, _ := primitive.PrevRegion.WordExtent()
	nextStart, _ := primitive.NextRegion.WordExtent()

	prevID := (*value)[prevStart]
	nextID := (*value)[nextStart]

	if prevID == 0 && nextID == 0 {
		prevID = (*value)[assetStart]
		nextID = (*value)[assetStart+1]
	}

	if prevID != 0 {
		ev.Meta["prev_id"] = FormatValueIDHex(prevID)
	}

	if nextID != 0 {
		ev.Meta["next_id"] = FormatValueIDHex(nextID)
	}
}

/*
QueueSubmitEvent fires when the pool queue accepts a new work item. Meta
prev_id/next_id are read from the live Value (same words as the following
WireFrameValue).
*/
func QueueSubmitEvent(inflight int64, value *primitive.Value, content string) Event {
	ev := NewEvent(EventQueueSubmit, "queue")
	ev.Label = truncate(content, 128)
	ev.Values["inflight"] = float64(inflight)
	ev.Meta["program"] = "affinity"

	if value == nil {
		applyVizLayoutQueue(&ev, inflight, 0)

		return ev
	}

	ev.Meta["value_id"] = FormatValueIDHex(value.ID())
	queueSubmitChainMeta(&ev, value)
	applyVizLayoutQueue(&ev, inflight, value.ID())

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
		"value_id": FormatValueIDHex(valueID),
		"mask":     strconv.FormatUint(mask, 16),
		"status":   status,
	}
	applyVizLayout(&ev, "causal_hub", vizBandQueue, fmt.Sprintf("%s|%d|%s", FormatValueIDHex(valueID), depth, status))
	return ev
}

func HolographicCrossoverEvent(valueID uint64) Event {
	ev := NewEvent(EventHolographicCrossover, "queue")
	applyVizLayout(&ev, "crossover", vizBandQueue, FormatValueIDHex(valueID))
	return ev
}

func SenseEvent(valueID uint64, amplitude, index int) Event {
	ev := NewEvent(EventSense, "queue")
	applyVizLayout(&ev, "sense", vizBandQueue, fmt.Sprintf("%s|%d|%d", FormatValueIDHex(valueID), amplitude, index))
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
	ev.Meta = map[string]string{"value_id": FormatValueIDHex(valueID)}
	applyVizLayoutCommunity(&ev, communityID, fmt.Sprintf("join|%s", FormatValueIDHex(valueID)))
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
	idHex := FormatValueIDHex(actionID)
	ev.Meta = map[string]string{"action_id": idHex}
	applyVizLayoutProgram(&ev, communityID, program, idHex)
	return ev
}

func CommunityReactionEvent(communityID int, reactionID uint64, program string) Event {
	ev := NewEvent(EventCommunityReaction, "orchestrator")
	ev.Label = program
	ev.Values = map[string]float64{"community_id": float64(communityID)}
	idHex := FormatValueIDHex(reactionID)
	ev.Meta = map[string]string{"reaction_id": idHex}
	applyVizLayoutProgram(&ev, communityID, program, idHex)
	return ev
}

// --- Belief Gap Closure ---

func BeliefGapEvaluatedEvent(valueID uint64, communityID int, gap float64) Event {
	ev := NewEvent(EventBeliefGapEvaluated, "orchestrator")
	ev.Values = map[string]float64{
		"community_id": float64(communityID),
		"gap":          gap,
	}
	ev.Meta = map[string]string{"value_id": FormatValueIDHex(valueID)}
	applyVizLayoutCommunity(&ev, communityID, fmt.Sprintf("gap|%s|%0.4f", FormatValueIDHex(valueID), gap))
	return ev
}

func ValueResolvedEvent(valueID uint64, communityID int, gap float64) Event {
	ev := NewEvent(EventValueResolved, "orchestrator")
	ev.Label = "resolved"
	ev.Values = map[string]float64{
		"community_id": float64(communityID),
		"gap":          gap,
	}
	ev.Meta = map[string]string{"value_id": FormatValueIDHex(valueID)}
	applyVizLayoutCommunity(&ev, communityID, fmt.Sprintf("resolved|%s", FormatValueIDHex(valueID)))
	return ev
}

func CommunityEmissionEvent(communityID int, memberCount int, concentration float64) Event {
	ev := NewEvent(EventCommunityEmission, "orchestrator")
	ev.Values = map[string]float64{
		"community_id":  float64(communityID),
		"member_count":  float64(memberCount),
		"concentration": concentration,
	}
	applyVizLayoutCommunity(&ev, communityID, fmt.Sprintf("emit|%d|%0.3f", memberCount, concentration))
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

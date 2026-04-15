package telemetry

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
EventKind identifies the category of a telemetry event.
The numeric ids intentionally stay aligned with the visualizer's wire decoder.
*/
type EventKind uint8

const (
	EventNodeCreated EventKind = iota
	EventNodeUpdated
	EventNodeRemoved

	EventPeerAdded
	EventPeerRemoved
	EventPeerLatency

	EventValuePublished
	EventValueReplicated
	EventGossipSent
	EventGossipReceived

	EventFieldDigest
	EventEigenmodeDetected
	EventFieldPressure

	EventTrieInsert
	EventTrieDecay
	EventTriePrune
	EventTriePredict
	EventTrieClassify
	EventTrieExperience

	EventPoolSchedule
	EventPoolComplete

	EventAdaptiveUpdate

	EventTrieCoupling
	EventTrieMode
	EventTriePressure
	EventTrieSignal

	EventBeamCollect
	EventBeamCompose
	EventBeamBreak
	EventBeamConverge

	EventPrompt
	EventPromptResult

	EventTrieGraphSnapshot

	EventCompilerCompile
	EventALUDispatch
	EventFinalizerRun

	EventDatasetRead
	EventTokenizerChunk
	EventTokenizerEmit
	EventQueueSubmit
	EventHolographicCrossover
	EventSense

	EventCommunityCreated
	EventValueJoinedCommunity
	EventCommunitySaturated
	EventCommunityAction
	EventCommunityReaction
	EventCausalHubProbe

	EventBeliefGapEvaluated
	EventValueResolved
	EventCommunityEmission
)

/*
Event is one telemetry datum.
All fields are value types so callers can safely queue and forward copies.
*/
type Event struct {
	Kind      EventKind          `json:"kind"`
	Timestamp int64              `json:"ts"`
	Source    string             `json:"src"`
	Target    string             `json:"tgt"`
	Label     string             `json:"lbl"`
	Values    map[string]float64 `json:"vals"`
	Meta      map[string]string  `json:"meta"`
}

/*
NewEvent stamps an event with the current unix microsecond timestamp.
*/
func NewEvent(kind EventKind, source string) Event {
	return Event{
		Kind:      kind,
		Timestamp: time.Now().UnixMicro(),
		Source:    source,
		Values:    make(map[string]float64),
		Meta:      make(map[string]string),
	}
}

/*
FormatValueIDHex renders a Value id as fixed-width lower-case hex.
*/
func FormatValueIDHex(id uint64) string {
	return fmt.Sprintf("%016x", id)
}

/*
AffinityHexFromFrame flattens the affinity words into one lower-case hex string.
*/
func AffinityHexFromFrame(value *primitive.Value) string {
	if value == nil {
		return ""
	}

	var builder strings.Builder
	builder.Grow(5 * 16)

	for index := range 5 {
		fmt.Fprintf(&builder, "%016x", (*value)[kernel.AffinityStartWord+index])
	}

	return builder.String()
}

/*
GossipSent records a gossip send with the originating node and epoch.
*/
func GossipSent(from uint64, epoch uint64) Event {
	event := NewEvent(EventGossipSent, fmtNodeID(from))
	event.Values["epoch"] = float64(epoch)

	return event
}

/*
GossipReceived records a gossip receive with the destination node and source node.
*/
func GossipReceived(to uint64, from uint64, epoch uint64) Event {
	event := NewEvent(EventGossipReceived, fmtNodeID(to))
	event.Target = fmtNodeID(from)
	event.Values["epoch"] = float64(epoch)

	return event
}

/*
PoolScheduleEvent records a substrate dispatch entering the pool.
*/
func PoolScheduleEvent(action string, queueSize, workers int, valueID uint64) Event {
	event := NewEvent(EventPoolSchedule, "pool")
	event.Label = action
	event.Values["inflight"] = float64(queueSize)
	event.Values["queue_size"] = float64(queueSize)
	event.Values["workers"] = float64(workers)
	event.Meta["value_id"] = FormatValueIDHex(valueID)

	return event
}

/*
PoolCompleteEvent records a substrate dispatch finishing in the pool.
*/
func PoolCompleteEvent(action string, durationNanos int64, valueID uint64) Event {
	event := NewEvent(EventPoolComplete, "pool")
	event.Label = action
	event.Values["duration_ms"] = float64(durationNanos) / 1_000_000.0
	event.Values["duration_ns"] = float64(durationNanos)
	event.Meta["value_id"] = FormatValueIDHex(valueID)

	return event
}

/*
CompilerCompileEvent records a queued compile before ALU execution.
*/
func CompilerCompileEvent(
	targetLabel string,
	operation uint64,
	correlation uint64,
	compileNanos int64,
	batchAffinity bool,
	finalizerDepth int,
) Event {
	event := NewEvent(EventCompilerCompile, "compute")
	event.Label = targetLabel
	event.Values["operation"] = float64(operation)
	event.Values["compile_ns"] = float64(compileNanos)
	event.Values["finalizer_depth"] = float64(finalizerDepth)

	if batchAffinity {
		event.Values["batch_affinity"] = 1
	}

	if correlation != 0 {
		event.Meta["correlation"] = strconv.FormatUint(correlation, 10)
	}

	return event
}

/*
ALUDispatchEvent records substrate execution after the opcode is fixed.
*/
func ALUDispatchEvent(substrateName string, opcode uint8, correlation uint64, durationNanos int64, valueID uint64) Event {
	event := NewEvent(EventALUDispatch, "compute")
	event.Label = substrateName
	event.Values["opcode"] = float64(opcode)
	event.Values["duration_ms"] = float64(durationNanos) / 1_000_000.0
	event.Values["duration_ns"] = float64(durationNanos)
	event.Meta["substrate"] = substrateName
	event.Meta["value_id"] = FormatValueIDHex(valueID)

	if correlation != 0 {
		event.Meta["correlation"] = strconv.FormatUint(correlation, 10)
	}

	return event
}

/*
PromptEvent records a prompt entering the system.
*/
func PromptEvent(prompt string) Event {
	event := NewEvent(EventPrompt, "user")
	event.Meta["prompt"] = prompt

	return event
}

/*
PromptResultEvent records the prompt generation and optional scores.
*/
func PromptResultEvent(generation string, scores map[string]float64) Event {
	event := NewEvent(EventPromptResult, "system")
	event.Meta["generation"] = truncate(generation, 256)

	if len(scores) > 0 {
		event.Values = scores
	}

	return event
}

/*
DatasetReadEvent records one dataset chunk entering the tokenizer.
*/
func DatasetReadEvent(datasetName string, bytesRead, totalBytes int64, label string) Event {
	event := NewEvent(EventDatasetRead, "dataset")
	event.Label = label
	event.Values["bytes_read"] = float64(bytesRead)
	event.Values["total_bytes"] = float64(totalBytes)
	event.Meta["dataset"] = datasetName

	return event
}

/*
TokenizerChunkEvent records tokenizer ingress bytes before segmentation.
*/
func TokenizerChunkEvent(bytesWritten int) Event {
	event := NewEvent(EventTokenizerChunk, "tokenizer")
	event.Values["bytes_written"] = float64(bytesWritten)

	return event
}

/*
TokenizerEmitEvent records a freshly minted value segment and its readable content.
*/
func TokenizerEmitEvent(segment *primitive.Value, label string) Event {
	event := NewEvent(EventTokenizerEmit, "tokenizer")
	if segment == nil {
		return event
	}

	event.Label = label
	event.Meta["value_id"] = FormatValueIDHex(segment.ID())
	event.Meta["content"] = truncate(segment.String(), 128)
	event.Meta["program"] = "affinity"
	event.Meta["affinity"] = AffinityHexFromFrame(segment)

	return event
}

/*
QueueSubmitEvent records a value entering the orchestrator / queue path.
It captures staged chain ids before the link firmware commits them.
*/
func QueueSubmitEvent(inflight int64, value *primitive.Value, content string) Event {
	event := NewEvent(EventQueueSubmit, "queue")
	event.Label = truncate(content, 128)
	event.Values["inflight"] = float64(inflight)

	if value == nil {
		return event
	}

	event.Meta["value_id"] = FormatValueIDHex(value.ID())
	event.Meta["program"] = "affinity"

	prevID := (*value)[kernel.PrevStartWord]
	nextID := (*value)[kernel.NextStartWord]

	assetStart, _ := primitive.AssetRegion.WordExtent()

	if prevID == 0 {
		prevID = (*value)[assetStart]
	}

	if nextID == 0 {
		nextID = (*value)[assetStart+1]
	}

	if prevID != 0 {
		event.Meta["prev_id"] = FormatValueIDHex(prevID)
	}

	if nextID != 0 {
		event.Meta["next_id"] = FormatValueIDHex(nextID)
	}

	return event
}

/*
CommunityCreatedEvent records a new community field with its initial affinity.
*/
func CommunityCreatedEvent(communityID int, initialAffinity []uint64) Event {
	event := NewEvent(EventCommunityCreated, "orchestrator")
	event.Values["community_id"] = float64(communityID)

	if len(initialAffinity) > 0 {
		var builder strings.Builder
		builder.Grow(len(initialAffinity) * 16)

		for _, word := range initialAffinity {
			fmt.Fprintf(&builder, "%016x", word)
		}

		event.Meta["initial_affinity"] = builder.String()
	}

	return event
}

/*
ValueJoinedCommunityEvent records a value routing into a community.
*/
func ValueJoinedCommunityEvent(valueID uint64, communityID int, distance int) Event {
	event := NewEvent(EventValueJoinedCommunity, "orchestrator")
	event.Values["community_id"] = float64(communityID)
	event.Values["distance"] = float64(distance)
	event.Meta["value_id"] = FormatValueIDHex(valueID)

	return event
}

func fmtNodeID(id uint64) string {
	return fmt.Sprintf("node_%x", id)
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}

	return value[:max]
}

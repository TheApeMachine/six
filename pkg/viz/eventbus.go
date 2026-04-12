package viz

import (
	"sync"
	"sync/atomic"
	"time"
)

// EventKind identifies the category of a visualization event.
type EventKind uint8

const (
	// Node lifecycle.
	EventNodeCreated EventKind = iota
	EventNodeUpdated
	EventNodeRemoved

	// Peer / connection events.
	EventPeerAdded
	EventPeerRemoved
	EventPeerLatency

	// Data flow.
	EventValuePublished
	EventValueReplicated
	EventGossipSent
	EventGossipReceived

	// Field dynamics.
	EventFieldDigest
	EventEigenmodeDetected
	EventFieldPressure

	// MarkovTrie.
	EventTrieInsert
	EventTrieDecay
	EventTriePrune
	EventTriePredict
	EventTrieClassify
	EventTrieExperience

	// Compute pool.
	EventPoolSchedule
	EventPoolComplete

	// Adaptive state.
	EventAdaptiveUpdate

	// Intra-node field dynamics (trie-to-trie within a single node).
	EventTrieCoupling // Coupling strength between two local tries
	EventTrieMode     // Eigenmode membership assignment for a trie
	EventTriePressure // Asymmetric decay/learn pressure applied to a trie
	EventTrieSignal   // Per-trie signal snapshot (surprisal, entropy, growth)

	// Hierarchical beam search.
	EventBeamCollect  // Node collected continuations from tries
	EventBeamCompose  // Node-level beam selected winners
	EventBeamBreak    // Node broke a trie's beam (backtracking)
	EventBeamConverge // Node-level beam converged on final output

	// User interaction.
	EventPrompt
	EventPromptResult

	// Exact Markov trie topology (bounded snapshot for debugging).
	EventTrieGraphSnapshot

	// Compiler → ALU → finalizer pipeline (pkg/compute/programmer, backend.Execute).
	EventCompilerCompile
	EventALUDispatch
	EventFinalizerRun

	// Dataset → Tokenizer ingest pipeline.
	EventDatasetRead
	EventTokenizerChunk
	EventTokenizerEmit
	EventQueueSubmit
	EventHolographicCrossover
	EventSense

	// Orchestrator / Community Routing.
	EventCommunityCreated
	EventValueJoinedCommunity
	EventCommunitySaturated
	EventCommunityAction
	EventCommunityReaction
	EventCausalHubProbe
)

// Event is a single visualization datum. All fields are value types so events
// can be safely queued, serialized, and replayed without data races.
type Event struct {
	Kind      EventKind          `json:"kind"`
	Timestamp int64              `json:"ts"`   // unix microseconds
	Source    string             `json:"src"`  // originating component id
	Target    string             `json:"tgt"`  // optional target component id
	Label     string             `json:"lbl"`  // human-readable summary
	Values    map[string]float64 `json:"vals"` // numeric payload
	Meta      map[string]string  `json:"meta"` // string payload
}

// Now returns a microsecond timestamp.
func now() int64 { return time.Now().UnixMicro() }

// NewEvent creates an event stamped with the current time.
func NewEvent(kind EventKind, source string) Event {
	return Event{
		Kind:      kind,
		Timestamp: now(),
		Source:    source,
		Values:    make(map[string]float64),
		Meta:      make(map[string]string),
	}
}

// newEventWithMaps creates an event stamped with the current time and initializes the maps.
func newEventWithMaps(kind EventKind, source string) Event {
	return Event{
		Kind:      kind,
		Timestamp: now(),
		Source:    source,
		Values:    make(map[string]float64),
		Meta:      make(map[string]string),
	}
}

// subscriber is a non-blocking consumer of events.
type subscriber struct {
	ch     chan Event
	filter func(Event) bool // nil = accept all
}

// Bus is a lock-free, non-blocking event bus for visualization telemetry.
// Components publish events; the visualization server subscribes. When no
// subscribers exist, events are silently dropped — zero overhead.
type Bus struct {
	mu          sync.RWMutex
	subscribers []subscriber
	active      atomic.Bool
	dropped     atomic.Uint64
	seq         atomic.Uint64
}

// NewBus creates a dormant event bus. Call Activate() to start accepting events.
func NewBus() *Bus {
	return &Bus{}
}

// Activate enables event publishing. Until called, Publish is a no-op.
func (b *Bus) Activate() { b.active.Store(true) }

// Deactivate stops event publishing.
func (b *Bus) Deactivate() { b.active.Store(false) }

// IsActive reports whether the bus is accepting events.
func (b *Bus) IsActive() bool { return b.active.Load() }

// Dropped returns the total number of events dropped due to slow subscribers.
func (b *Bus) Dropped() uint64 { return b.dropped.Load() }

// Subscribe registers a new event consumer. The returned channel receives
// events until Unsubscribe is called. bufSize controls backpressure; when
// the channel is full, events are dropped for this subscriber.
// filter may be nil (accept all).
func (b *Bus) Subscribe(bufSize int, filter func(Event) bool) chan Event {
	if bufSize <= 0 {
		bufSize = 4096
	}

	ch := make(chan Event, bufSize)

	b.mu.Lock()
	b.subscribers = append(b.subscribers, subscriber{ch: ch, filter: filter})
	b.mu.Unlock()

	return ch
}

// Unsubscribe removes a previously subscribed channel.
func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, s := range b.subscribers {
		if s.ch == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends an event to all subscribers. Non-blocking: if a subscriber's
// channel is full, the event is dropped for that subscriber.
func (b *Bus) Publish(ev Event) {
	if !b.active.Load() {
		return
	}

	b.seq.Add(1)

	b.mu.RLock()
	subs := b.subscribers
	b.mu.RUnlock()

	for _, s := range subs {
		if s.filter != nil && !s.filter(ev) {
			continue
		}

		select {
		case s.ch <- ev:
		default:
			b.dropped.Add(1)
		}
	}
}

// DefaultBus is the global visualization event bus. Components import viz
// and call viz.DefaultBus.Publish(). When the viz server isn't running,
// the bus is inactive and Publish is a no-op branch.
var DefaultBus = NewBus()

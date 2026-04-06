package viz

import "sync"

// Timeline is a ring-buffer of events supporting scrubbing and snapshots.
type Timeline struct {
	mu     sync.RWMutex
	events []Event
	head   int // next write position
	count  int // total events stored (may exceed cap, wraps)
	cap    int
}

// NewTimeline creates a timeline with the given capacity.
func NewTimeline(capacity int) *Timeline {
	if capacity <= 0 {
		capacity = 100_000
	}
	return &Timeline{
		events: make([]Event, capacity),
		cap:    capacity,
	}
}

// Record appends an event to the timeline.
func (t *Timeline) Record(ev Event) {
	t.mu.Lock()
	t.events[t.head] = ev
	t.head = (t.head + 1) % t.cap
	if t.count < t.cap {
		t.count++
	}
	t.mu.Unlock()
}

// Len returns the number of events in the timeline.
func (t *Timeline) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.count
}

// Range returns events from index `from` to `to` (exclusive) in insertion order.
func (t *Timeline) Range(from, to int) []Event {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if from < 0 {
		from = 0
	}
	if to > t.count {
		to = t.count
	}
	if from >= to {
		return nil
	}

	result := make([]Event, 0, to-from)

	// Compute the ring-buffer start offset.
	start := 0
	if t.count == t.cap {
		start = t.head // oldest event is at head when full
	}

	for i := from; i < to; i++ {
		idx := (start + i) % t.cap
		result = append(result, t.events[idx])
	}

	return result
}

// Snapshot returns all events in chronological order.
func (t *Timeline) Snapshot() []Event {
	return t.Range(0, t.Len())
}

// Load replaces the timeline with a saved snapshot.
func (t *Timeline) Load(events []Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.count = 0
	t.head = 0

	for _, ev := range events {
		if t.count >= t.cap {
			break
		}
		t.events[t.head] = ev
		t.head = (t.head + 1) % t.cap
		t.count++
	}
}

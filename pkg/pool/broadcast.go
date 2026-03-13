package pool

import (
	"sync"
	"time"
)

// FilterFunc decides whether a result should be forwarded to a subscriber.
type FilterFunc func(*Result) bool

// RoutingRule combines a subscriber filter with a priority for message routing.
type RoutingRule struct {
	SubscriberID string
	Filter       FilterFunc
	Priority     int
}

// BroadcastGroup is a pub/sub fan-out for pool results.
type BroadcastGroup struct {
	mu sync.RWMutex

	ID           string
	subscribers  map[string]chan *Result
	filters      []FilterFunc
	routingRules map[string][]RoutingRule
	metrics      *BroadcastMetrics

	TTL          time.Duration
	LastUsed     time.Time
	maxQueueSize int
	done         chan struct{}
	closed       bool
}

type BroadcastMetrics struct {
	MessagesSent      int64
	MessagesDropped   int64
	AverageLatency    time.Duration
	TotalLatency      time.Duration
	BroadcastCount    int64
	ActiveSubscribers int
	LastBroadcastTime time.Time
}

// NewBroadcastGroup creates a group with the given TTL and max queue depth.
func NewBroadcastGroup(
	id string, ttl time.Duration, maxQueue int,
) *BroadcastGroup {
	return &BroadcastGroup{
		ID:           id,
		subscribers:  make(map[string]chan *Result),
		routingRules: make(map[string][]RoutingRule),
		TTL:          ttl,
		LastUsed:     time.Now(),
		maxQueueSize: maxQueue,
		metrics:      &BroadcastMetrics{},
		done:         make(chan struct{}),
	}
}

// Subscribe registers a new subscriber and returns its receive channel.
func (bg *BroadcastGroup) Subscribe(
	subscriberID string, bufferSize int, rules ...RoutingRule,
) chan *Result {
	bg.mu.Lock()
	defer bg.mu.Unlock()

	ch := make(chan *Result, bufferSize)
	bg.subscribers[subscriberID] = ch

	if len(rules) > 0 {
		bg.routingRules[subscriberID] = rules
	}

	bg.metrics.ActiveSubscribers++
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (bg *BroadcastGroup) Unsubscribe(subscriberID string) {
	bg.mu.Lock()
	defer bg.mu.Unlock()

	if ch, exists := bg.subscribers[subscriberID]; exists {
		close(ch)
		delete(bg.subscribers, subscriberID)
		delete(bg.routingRules, subscriberID)
		bg.metrics.ActiveSubscribers--
	}
}

func (bg *BroadcastGroup) Send(r *Result) {
	bg.mu.RLock()
	if bg.closed {
		bg.mu.RUnlock()
		return
	}
	startTime := time.Now()

	for _, filter := range bg.filters {
		if !filter(r) {
			bg.mu.RUnlock()
			bg.mu.Lock()
			bg.metrics.MessagesDropped++
			bg.LastUsed = startTime
			bg.mu.Unlock()
			return
		}
	}

	targets := make([]chan *Result, 0, len(bg.subscribers))

	for subID, ch := range bg.subscribers {
		if rules, hasRules := bg.routingRules[subID]; hasRules {
			shouldSend := false
			for _, rule := range rules {
				if rule.Filter(r) {
					shouldSend = true
					break
				}
			}
			if !shouldSend {
				continue
			}
		}
		targets = append(targets, ch)
	}
	bg.mu.RUnlock()

	var sent, dropped int64
	for _, ch := range targets {
		select {
		case ch <- r:
			sent++
		default:
			dropped++
		}
	}

	latency := time.Since(startTime)

	bg.mu.Lock()
	defer bg.mu.Unlock()
	bg.LastUsed = startTime
	bg.metrics.MessagesSent += sent
	bg.metrics.MessagesDropped += dropped
	bg.metrics.LastBroadcastTime = startTime

	bg.metrics.BroadcastCount++
	bg.metrics.TotalLatency += latency
	bg.metrics.AverageLatency = bg.metrics.TotalLatency / time.Duration(bg.metrics.BroadcastCount)
}

// AddFilter registers a global filter applied before broadcasting.
func (bg *BroadcastGroup) AddFilter(filter FilterFunc) {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	bg.filters = append(bg.filters, filter)
}

// AddRoutingRule adds a per-subscriber routing rule.
func (bg *BroadcastGroup) AddRoutingRule(subscriberID string, rule RoutingRule) {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	bg.routingRules[subscriberID] = append(bg.routingRules[subscriberID], rule)
}

// GetMetrics returns a copy of the current broadcast metrics.
func (bg *BroadcastGroup) GetMetrics() BroadcastMetrics {
	bg.mu.RLock()
	defer bg.mu.RUnlock()
	return *bg.metrics
}

// Close shuts down the group and closes all subscriber channels safely.
func (bg *BroadcastGroup) Close() {
	bg.mu.Lock()
	if bg.closed {
		bg.mu.Unlock()
		return
	}
	bg.closed = true
	close(bg.done)
	
	for _, ch := range bg.subscribers {
		close(ch)
	}

	bg.subscribers = nil
	bg.routingRules = nil
	bg.filters = nil
	bg.mu.Unlock()
}

package pool

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ResultStore manages job results and subscriber notifications.
//
// It stores results keyed by job ID, notifies awaiting callers when
// results arrive, and cleans up expired entries periodically.
// Uses sync.Map for values to reduce lock contention on Exists (hot path).
type ResultStore struct {
	mu sync.RWMutex

	values  sync.Map // map[string]*Result — sync.Map for high read concurrency
	waiting map[string][]chan *Result
	groups  map[string]*BroadcastGroup

	children map[string][]string
	parents  map[string][]string

	cleanupInterval time.Duration
	wg              sync.WaitGroup
	done            chan struct{}
}

// NewResultStore creates a store with periodic cleanup.
func NewResultStore() *ResultStore {
	rs := &ResultStore{
		waiting:         make(map[string][]chan *Result),
		groups:          make(map[string]*BroadcastGroup),
		children:        make(map[string][]string),
		parents:         make(map[string][]string),
		cleanupInterval: time.Minute,
		done:            make(chan struct{}),
	}

	rs.wg.Add(1)
	go rs.runCleanup()

	return rs
}

// Store saves a result and notifies any waiters.
func (rs *ResultStore) Store(id string, value any, ttl time.Duration) {
	r := NewResult(value)
	r.TTL = ttl
	rs.values.Store(id, r)

	rs.mu.Lock()
	defer rs.mu.Unlock()

	if channels, ok := rs.waiting[id]; ok {
		for _, ch := range channels {
			select {
			case ch <- r:
			default:
				rs.removeWaitingChannel(id, ch)
			}
		}
		delete(rs.waiting, id)
	}
}

// StoreError saves an error result and notifies waiters.
func (rs *ResultStore) StoreError(id string, err error, ttl time.Duration) {
	r := NewResult(nil)
	r.Error = err
	r.TTL = ttl
	rs.values.Store(id, r)

	rs.mu.Lock()
	defer rs.mu.Unlock()

	if channels, ok := rs.waiting[id]; ok {
		for _, ch := range channels {
			select {
			case ch <- r:
			default:
				rs.removeWaitingChannel(id, ch)
			}
		}
		delete(rs.waiting, id)
	}
}

// Await returns a channel that receives the result when available.
func (rs *ResultStore) Await(id string) chan *Result {
	ch := make(chan *Result, 1)

	if v, ok := rs.values.Load(id); ok {
		ch <- v.(*Result)
		close(ch)
		return ch
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()

	if v, ok := rs.values.Load(id); ok {
		ch <- v.(*Result)
		close(ch)
		return ch
	}
	rs.waiting[id] = append(rs.waiting[id], ch)
	return ch
}

// Exists checks whether a result for the given ID has been stored.
// Lock-free via sync.Map — hot path for dependency checks.
func (rs *ResultStore) Exists(id string) bool {
	_, exists := rs.values.Load(id)
	return exists
}

// AddRelationship establishes a parent-child link between job IDs,
// rejecting cycles.
func (rs *ResultStore) AddRelationship(parentID, childID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.wouldCreateCircle(parentID, childID) {
		return fmt.Errorf("circular dependency detected")
	}

	rs.children[parentID] = append(rs.children[parentID], childID)
	rs.parents[childID] = append(rs.parents[childID], parentID)
	return nil
}

// CreateBroadcastGroup creates and registers a broadcast group.
func (rs *ResultStore) CreateBroadcastGroup(id string, ttl time.Duration) *BroadcastGroup {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if group, exists := rs.groups[id]; exists {
		return group
	}

	group := NewBroadcastGroup(id, ttl, 100)
	rs.groups[id] = group
	return group
}

// subscribeCounter generates unique subscriber IDs so each Subscribe gets its own channel.
var subscribeCounter atomic.Uint64

// Subscribe returns a channel for receiving values from a broadcast group.
// Each call gets a unique subscriber ID; previously Subscribe used "" which overwrote
// the prior subscriber, leaving only the last one receiving messages.
func (rs *ResultStore) Subscribe(groupID string) chan *Result {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if group, exists := rs.groups[groupID]; exists {
		id := fmt.Sprintf("sub-%d", subscribeCounter.Add(1))
		return group.Subscribe(id, 10)
	}
	return nil
}

// Close shuts down the store and releases resources.
func (rs *ResultStore) Close() {
	close(rs.done)
	rs.wg.Wait()

	rs.mu.Lock()
	defer rs.mu.Unlock()

	for _, channels := range rs.waiting {
		for _, ch := range channels {
			close(ch)
		}
	}

	for _, group := range rs.groups {
		group.Close()
	}

	rs.waiting = nil
	rs.groups = nil
	rs.children = nil
	rs.parents = nil
}

func (rs *ResultStore) runCleanup() {
	defer rs.wg.Done()
	ticker := time.NewTicker(rs.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rs.done:
			return
		case <-ticker.C:
			rs.cleanup()
		}
	}
}

func (rs *ResultStore) cleanup() {
	now := time.Now()
	var expired []string
	rs.values.Range(func(key, value any) bool {
		id := key.(string)
		r := value.(*Result)
		if r.TTL > 0 && now.Sub(r.CreatedAt) > r.TTL {
			expired = append(expired, id)
		}
		return true
	})
	if len(expired) > 0 {
		rs.mu.Lock()
		for _, id := range expired {
			v, ok := rs.values.Load(id)
			if !ok {
				continue
			}
			r := v.(*Result)
			if r.TTL <= 0 || now.Sub(r.CreatedAt) <= r.TTL {
				continue
			}
			rs.values.Delete(id)
			delete(rs.children, id)
			for _, parentID := range rs.parents[id] {
				rs.children[parentID] = removeString(rs.children[parentID], id)
			}
			delete(rs.parents, id)
		}
		rs.mu.Unlock()
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()

	for id, group := range rs.groups {
		if group.TTL > 0 && now.Sub(group.LastUsed) > group.TTL {
			group.Close()
			delete(rs.groups, id)
		}
	}
}

func (rs *ResultStore) removeWaitingChannel(id string, ch chan *Result) {
	channels := rs.waiting[id]
	for i, waitingCh := range channels {
		if waitingCh == ch {
			rs.waiting[id] = append(channels[:i], channels[i+1:]...)
			return
		}
	}
}

func (rs *ResultStore) wouldCreateCircle(parentID, childID string) bool {
	visited := make(map[string]bool)
	var check func(string) bool

	check = func(current string) bool {
		if current == parentID {
			return true
		}
		if visited[current] {
			return false
		}
		visited[current] = true

		for _, parent := range rs.parents[current] {
			if check(parent) {
				return true
			}
		}
		return false
	}

	return check(childID)
}

func removeString(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

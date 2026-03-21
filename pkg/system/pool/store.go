package pool

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

/*
ResultStore manages job results and subscriber notifications.
Stores results keyed by job ID and cleans up expired entries periodically.
Uses sync.Map for values to reduce lock contention on Exists (hot path).
*/
type ResultStore struct {
	mu sync.RWMutex

	values sync.Map // map[string]*Result — sync.Map for high read concurrency
	groups map[string]*BroadcastGroup

	children map[string][]string
	parents  map[string][]string

	cleanupInterval time.Duration
	wg              sync.WaitGroup
	done            chan struct{}
	closeOnce       sync.Once

	streamMu     sync.Mutex
	streamCond   *sync.Cond
	streamBuffer bytes.Buffer
	streamClosed bool
}

var errResultStoreClosed = errors.New("result store stream is closed")

/*
NewResultStore creates a store with periodic cleanup.
*/
func NewResultStore() *ResultStore {
	rs := &ResultStore{
		groups:          make(map[string]*BroadcastGroup),
		children:        make(map[string][]string),
		parents:         make(map[string][]string),
		cleanupInterval: time.Minute,
		done:            make(chan struct{}),
	}
	rs.streamCond = sync.NewCond(&rs.streamMu)

	rs.wg.Add(1)
	go rs.runCleanup()

	return rs
}

/*
Read drains bytes from the store stream.
*/
func (rs *ResultStore) Read(p []byte) (n int, err error) {
	rs.streamMu.Lock()
	defer rs.streamMu.Unlock()

	for rs.streamBuffer.Len() == 0 && !rs.streamClosed {
		rs.streamCond.Wait()
	}

	if rs.streamBuffer.Len() > 0 {
		return rs.streamBuffer.Read(p)
	}

	return 0, io.EOF
}

/*
Write appends bytes to the store stream.
*/
func (rs *ResultStore) Write(p []byte) (n int, err error) {
	rs.streamMu.Lock()
	defer rs.streamMu.Unlock()

	if rs.streamClosed {
		return 0, errResultStoreClosed
	}

	n, err = rs.streamBuffer.Write(p)

	if n > 0 {
		rs.streamCond.Broadcast()
	}

	return n, err
}

/*
Store saves a result and notifies any waiters.
*/
func (rs *ResultStore) Store(id string, value any, ttl time.Duration) {
	result := NewResult(value)
	result.TTL = ttl
	rs.values.Store(id, result)
}

/*
StoreError saves an error result and notifies waiters.
*/
func (rs *ResultStore) StoreError(id string, err error, ttl time.Duration) {
	r := NewResult(nil)
	r.Error = err
	r.TTL = ttl
	rs.values.Store(id, r)
}

/*
Result returns a stored result when available.
*/
func (rs *ResultStore) Result(id string) (*Result, bool) {
	value, ok := rs.values.Load(id)
	if !ok {
		return nil, false
	}

	return value.(*Result), true
}

/*
Exists checks whether a result for the given ID has been stored.
Lock-free via sync.Map — hot path for dependency checks.
*/
func (rs *ResultStore) Exists(id string) bool {
	_, exists := rs.values.Load(id)
	return exists
}

/*
AddRelationship establishes a parent-child link between job IDs,
rejecting cycles.
*/
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

/*
CreateBroadcastGroup creates and registers a broadcast group.
*/
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

/*
subscribeCounter generates unique subscriber IDs so each Subscribe gets its own channel.
*/
var subscribeCounter atomic.Uint64

/*
Subscribe returns a channel for receiving values from a broadcast group.
Each call gets a unique subscriber ID; previously Subscribe used "" which overwrote
the prior subscriber, leaving only the last one receiving messages.
*/
func (rs *ResultStore) Subscribe(groupID string) chan *Result {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if group, exists := rs.groups[groupID]; exists {
		id := fmt.Sprintf("sub-%d", subscribeCounter.Add(1))
		return group.Subscribe(id, 10)
	}
	return nil
}

/*
Close shuts down the store and releases resources.
*/
func (rs *ResultStore) Close() error {
	rs.closeOnce.Do(func() {
		close(rs.done)
		rs.wg.Wait()

		rs.streamMu.Lock()
		rs.streamClosed = true
		rs.streamMu.Unlock()
		rs.streamCond.Broadcast()

		rs.mu.Lock()
		defer rs.mu.Unlock()

		for _, group := range rs.groups {
			group.Close()
		}
		rs.groups = nil
		rs.children = nil
		rs.parents = nil
	})

	return nil
}

/*
runCleanup runs the periodic cleanup loop.
*/
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

/*
cleanup removes expired results and TTL-exceeded broadcast groups.
*/
func (rs *ResultStore) cleanup() {
	now := time.Now()

	rs.mu.Lock()
	defer rs.mu.Unlock()

	var expired []string
	rs.values.Range(func(key, value any) bool {
		id := key.(string)
		r := value.(*Result)
		if r.TTL > 0 && now.Sub(r.CreatedAt) > r.TTL {
			expired = append(expired, id)
		}
		return true
	})

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
		parentIDs, hasParents := rs.parents[id]
		if hasParents {
			for _, parentID := range parentIDs {
				rs.children[parentID] = removeString(rs.children[parentID], id)
			}
		}
		delete(rs.parents, id)
	}

	for id, group := range rs.groups {
		if group.TTL > 0 && now.Sub(group.LastUsed) > group.TTL {
			group.Close()
			delete(rs.groups, id)
		}
	}
}

/*
AddChildDependency records that jobID depends on depID for await tracking.
*/
func (rs *ResultStore) AddChildDependency(depID, jobID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.children == nil || rs.parents == nil {
		return
	}
	rs.children[depID] = append(rs.children[depID], jobID)
}

/*
wouldCreateCircle returns true if adding parentID->childID would create a cycle.
*/
/*
wouldCreateCircle detects whether adding parentID->childID would close a directed
cycle along existing child edges (parent observes children).
*/
func (rs *ResultStore) wouldCreateCircle(parentID, childID string) bool {
	if parentID == childID {
		return true
	}

	visited := make(map[string]bool)
	var walk func(string) bool

	walk = func(node string) bool {
		if node == parentID {
			return true
		}

		if visited[node] {
			return false
		}

		visited[node] = true

		for _, child := range rs.children[node] {
			if walk(child) {
				return true
			}
		}

		return false
	}

	return walk(childID)
}

func removeString(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

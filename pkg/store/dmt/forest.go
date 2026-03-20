package dmt

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
Forest manages a collection of Tree instances, providing intelligent routing of operations
to the most performant tree based on running performance metrics. It maintains data
consistency across all trees while optimizing read operations by selecting the fastest
responding tree.
*/
type Forest struct {
	state *errnie.State
	trees []*Tree
	mu    sync.RWMutex
	// Channel to signal new updates that need synchronization
	updates chan struct{}
	// Context for controlling background sync
	ctx    context.Context
	cancel context.CancelFunc
	pool   *pool.Pool
	loops  *pool.Pool
	owned  bool
	// Network node for distributed operation
	network *NetworkNode
	// postUpdateSyncCount increments after each synchronizeTrees triggered by updates.
	postUpdateSyncCount atomic.Uint64
}

// ForestConfig holds configuration for creating a new Forest
type ForestConfig struct {
	// Directory for persistence
	PersistDir string
	// Worker pool for background tasks
	Pool *pool.Pool
	// Network configuration
	Network *NetworkConfig
}

/*
NewForest creates and returns a new empty Forest instance with background
synchronization enabled. The forest starts with no trees and trees can be
added using the AddTree method. A background goroutine is started to handle
tree synchronization.
*/
func NewForest(config ForestConfig) (*Forest, error) {
	ctx, cancel := context.WithCancel(context.Background())
	forest := &Forest{
		state:   errnie.NewState("dmt/forest"),
		updates: make(chan struct{}, 1), // Buffered channel to prevent blocking
		ctx:     ctx,
		cancel:  cancel,
		pool:    config.Pool,
		loops: pool.New(
			ctx,
			4,
			max(4, runtime.NumCPU()),
			&pool.Config{},
		),
	}

	if forest.pool == nil {
		forest.pool = pool.New(forest.ctx, 1, runtime.NumCPU(), &pool.Config{})
		forest.owned = true
	}

	// Create initial tree (with persistence if directory is provided)
	tree := errnie.Guard(forest.state, func() (*Tree, error) {
		return NewTree(config.PersistDir)
	})

	forest.AddTree(tree)

	// Initialize network node if network config provided
	if config.Network != nil {
		forest.network = errnie.Guard(forest.state, func() (*NetworkNode, error) {
			return NewNetworkNode(*config.Network, forest)
		})
	}

	forest.schedule("sync-loop", func(ctx context.Context) (any, error) {
		forest.syncLoop()
		return nil, nil
	})

	return forest, forest.state.Err()
}

/*
Close stops the background synchronization goroutine and cleans up resources.
*/
func (forest *Forest) Close() error {
	if forest.cancel != nil {
		forest.cancel()
	}

	forest.mu.Lock()
	defer forest.mu.Unlock()

	if forest.network != nil {
		errnie.GuardVoid(forest.state, forest.network.Close)
	}

	for _, tree := range forest.trees {
		errnie.GuardVoid(forest.state, tree.Close)
	}

	if forest.owned {
		forest.pool.Close()
	}

	if forest.loops != nil {
		forest.loops.Close()
	}

	return forest.state.Err()
}

/*
schedule runs a Forest background task on the managed worker pool.
*/
func (forest *Forest) schedule(
	id string,
	fn func(ctx context.Context) (any, error),
) {
	forest.pool.Schedule(
		"dmt/forest/"+id,
		pool.COMPUTE,
		&readPoolTask{ctx: forest.ctx, fn: fn},
		pool.WithContext(forest.ctx),
	)
}

func (forest *Forest) scheduleLoop(
	id string,
	fn func(ctx context.Context) (any, error),
) {
	forest.loops.Schedule(
		"dmt/forest/"+id,
		pool.COMPUTE,
		&readPoolTask{ctx: forest.ctx, fn: fn, loop: true},
		pool.WithContext(forest.ctx),
		pool.WithTTL(time.Second),
	)
}

/*
syncLoop runs in the background and handles synchronization of trees.
It is triggered either by updates or periodically to ensure consistency.
*/
func (forest *Forest) syncLoop() {
	ticker := time.NewTicker(5 * time.Second) // Periodic sync every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-forest.ctx.Done():
			return
		case <-forest.updates: // Triggered by new updates
			forest.synchronizeTrees()
			forest.postUpdateSyncCount.Add(1)
		case <-ticker.C: // Periodic sync
			forest.synchronizeTrees()
		}
	}
}

/*
initTreeMerkle lazy-initializes a Tree's Merkle companion by scanning all
current leaves. Subsequent inserts keep it incrementally up-to-date via the
Tree.Insert hook.
*/
func (forest *Forest) initTreeMerkle(tree *Tree) {
	tree.merkle = NewMerkleTree()

	it := tree.root.Root().Iterator()
	for key, value, ok := it.Next(); ok; key, value, ok = it.Next() {
		tree.merkle.Insert(key, value)
	}
}

/*
synchronizeTrees ensures all trees have consistent data by comparing and
updating them based on the most up-to-date tree. Merkle trees are kept
per-Tree and rebuilt only when the tree has been modified, avoiding a full
leaf scan on every sync cycle.
*/
func (forest *Forest) synchronizeTrees() {
	forest.mu.Lock()
	defer forest.mu.Unlock()

	if len(forest.trees) <= 1 {
		return
	}

	reference := forest.trees[0]

	if reference.merkle == nil {
		forest.initTreeMerkle(reference)
	}

	reference.merkle.Rebuild()

	for _, tree := range forest.trees[1:] {
		if tree.merkle == nil {
			forest.initTreeMerkle(tree)
		}

		tree.merkle.Rebuild()

		diffs := reference.merkle.GetDiff(tree.merkle)

		for _, diff := range diffs {
			tree.Insert(diff.Key, diff.Value)
		}
	}
}

/*
AddTree incorporates a new Tree instance into the forest.
Each added tree will be maintained with identical data but may have different
performance characteristics based on its specific implementation or state.
*/
func (forest *Forest) AddTree(tree *Tree) {
	forest.mu.Lock()
	forest.trees = append(forest.trees, tree)
	forest.mu.Unlock()
	// Trigger synchronization for the new tree
	select {
	case forest.updates <- struct{}{}:
	default:
	}
}

/*
getFastestTree returns the tree with the lowest average performance time.
It analyzes the running performance metrics of each tree to determine which
one is currently responding most quickly to operations. Returns nil if the
forest contains no trees.
*/
func (forest *Forest) getFastestTree() *Tree {
	forest.mu.RLock()
	defer forest.mu.RUnlock()

	if len(forest.trees) == 0 {
		return nil
	}

	fastestTree := forest.trees[0]
	fastestAvg := fastestTree.AVG()

	for _, tree := range forest.trees[1:] {
		if avg := tree.AVG(); avg < fastestAvg {
			fastestTree = tree
			fastestAvg = avg
		}
	}

	return fastestTree
}

/*
Get retrieves a value from the forest using the most performant tree.
It automatically selects the tree with the best average response time to
handle the request. Returns the value and true if the key exists, or nil
and false if it doesn't or if the forest is empty.
*/
func (forest *Forest) Get(key []byte) ([]byte, bool) {
	fastestTree := forest.getFastestTree()
	if fastestTree == nil {
		return nil, false
	}
	return fastestTree.Get(key)
}

/*
Seek performs a prefix-based search using the most performant tree in the forest.
It finds the first value whose key is greater than or equal to the provided key
in lexicographical order. Returns the value and true if found, or nil and false
if no such key exists or if the forest is empty.
*/
func (forest *Forest) Seek(key []byte) ([]byte, bool) {
	fastestTree := forest.getFastestTree()
	if fastestTree == nil {
		return nil, false
	}
	return fastestTree.Seek(key)
}

/*
Insert adds or updates a key-value pair across all trees in the forest.
To maintain data consistency, the operation is performed on every tree,
ensuring that subsequent read operations will find the same data regardless
of which tree they query. The lock is released before network broadcast so
reads are not blocked during I/O.
*/
func (forest *Forest) Insert(key []byte, value []byte) {
	forest.mu.Lock()

	for _, tree := range forest.trees {
		tree.Insert(key, value)
	}

	network := forest.network
	forest.mu.Unlock()

	if network != nil {
		network.BroadcastInsert(key, value)
	}
}

/*
Iterate walks all key-value pairs in the fastest tree, calling fn for each.
Stops early if fn returns false.
*/
func (forest *Forest) Iterate(fn func(key []byte, value []byte) bool) {
	tree := forest.getFastestTree()
	if tree == nil {
		return
	}

	tree.mu.RLock()
	defer tree.mu.RUnlock()

	it := tree.root.Root().Iterator()

	for key, value, ok := it.Next(); ok; key, value, ok = it.Next() {
		if !fn(key, value) {
			return
		}
	}
}

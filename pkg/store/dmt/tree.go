/*
package dmt implements a wrapper around an immutable radix tree data structure.
A radix tree (also known as a radix trie or compact prefix tree) is a space-optimized
tree structure that is particularly efficient for string or byte slice keys. It compresses
common prefixes to save space and enables fast lookups, insertions, and prefix-based searches.
*/
package dmt

import (
	"bytes"
	"container/ring"
	"sync"
	"time"

	iradix "github.com/hashicorp/go-immutable-radix/v2"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Tree wraps an immutable radix tree implementation from hashicorp/go-immutable-radix.
It stores byte slices as both keys and values, providing efficient prefix-based operations.
The immutable nature ensures thread-safety and enables persistent data structures.
*/
type Tree struct {
	state    *errnie.State
	root     *iradix.Tree[[]byte]
	updated  bool
	perfs    *ring.Ring
	persist  *PersistentStore
	term     uint64
	logIndex uint64
	mu       sync.RWMutex
}

/*
NewTree creates and returns a new empty Tree instance.
The underlying radix tree is initialized with no entries.
*/
func NewTree(persistDir string) (*Tree, error) {
	tree := &Tree{
		state: errnie.NewState("dmt/tree"),
		root:  iradix.New[[]byte](),
		perfs: ring.New(10),
	}

	if persistDir != "" {
		tree.persist = errnie.Guard(tree.state, func() (*PersistentStore, error) {
			return NewPersistentStore(persistDir)
		})

		entries := errnie.Guard(tree.state, tree.persist.Replay)

		for _, entry := range entries {
			tree.root, _, _ = tree.root.Insert(entry.Key, entry.Value)
		}

		tree.term, tree.logIndex = tree.persist.GetLastState()
	}

	return tree, tree.state.Err()
}

/*
Seek performs a prefix-based search in the tree, finding the first value whose key
is greater than or equal to the provided key in lexicographical order.
Returns the value and true if found, or nil and false if no such key exists.
*/
func (tree *Tree) Seek(key []byte) ([]byte, bool) {
	tree.mu.RLock()
	defer tree.mu.RUnlock()

	t := time.Now()

	it := tree.root.Root().Iterator()
	it.SeekLowerBound(key)

	for k, v, ok := it.Next(); ok; k, v, ok = it.Next() {
		if bytes.Compare(k, key) >= 0 {
			return v, true
		}
	}

	tree.perfs.Value = time.Since(t).Nanoseconds()
	tree.perfs = tree.perfs.Next()

	return nil, false
}

/*
Insert adds or updates a key-value pair in the tree.
Due to the immutable nature of the tree, this operation creates a new version
of the tree rather than modifying the existing one.
Returns the updated tree and a boolean indicating if the tree was modified.
*/
func (tree *Tree) Insert(key []byte, value []byte) (*Tree, bool) {
	tree.mu.Lock()
	defer tree.mu.Unlock()

	t := time.Now()
	key = append([]byte(nil), key...)
	value = append([]byte(nil), value...)
	oldRoot := tree.root
	tree.root, _, _ = tree.root.Insert(key, value)
	tree.updated = tree.root != oldRoot

	if tree.updated {
		tree.logIndex++

		if tree.persist != nil {
			errnie.GuardVoid(tree.state, func() error {
				return tree.persist.LogInsert(key, value, tree.term, tree.logIndex)
			})
		}
	}

	tree.perfs.Value = time.Since(t).Nanoseconds()
	tree.perfs = tree.perfs.Next()
	return tree, tree.updated
}

/*
Get retrieves the value associated with the given key.
Returns the value and true if the key exists, or nil and false if it doesn't.
*/
func (tree *Tree) Get(key []byte) ([]byte, bool) {
	tree.mu.RLock()
	defer tree.mu.RUnlock()

	t := time.Now()
	v, ok := tree.root.Get(key)
	tree.perfs.Value = time.Since(t).Nanoseconds()
	tree.perfs = tree.perfs.Next()
	return v, ok
}

/*
AVG returns the average performance of the tree in nanoseconds.
*/
func (tree *Tree) AVG() int64 {
	var sum int64
	var count int64

	tree.perfs.Do(func(v any) {
		if v == nil {
			return
		}

		sum += v.(int64)
		count++
	})

	if count == 0 {
		return 0
	}

	return sum / count
}

/*
Close closes the tree and persists any remaining data.
*/
func (tree *Tree) Close() error {
	if tree.persist != nil {
		errnie.GuardVoid(tree.state, tree.persist.Close)
	}
	return tree.state.Err()
}

func (tree *Tree) UpdateTerm(term uint64) {
	tree.mu.Lock()
	defer tree.mu.Unlock()

	tree.term = term

	if tree.persist != nil {
		errnie.GuardVoid(tree.state, func() error {
			return tree.persist.LogTerm(term)
		})
	}
}

// GetLogState returns the current term and log index
func (tree *Tree) GetLogState() (term, index uint64) {
	tree.mu.RLock()
	defer tree.mu.RUnlock()
	return tree.term, tree.logIndex
}

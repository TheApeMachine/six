package dmt

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
Proof entries prefix a sibling hash with proofPosLeft or proofPosRight so the
verifier hashes (left, right) consistently. proofPromoteNilRight marks a
missing right sibling at rebuild time; the following byte is proofNilRightPad.
*/
const (
	proofPosLeft         byte = 0x00
	proofPosRight        byte = 0x01
	proofPromoteNilRight byte = 0x02
	proofNilRightPad     byte = 0x00
)

/*
MerkleNode is one node in the tree. Leaves store Key/Value; internal nodes
store only Hash. Left/Right are nil for leaves.
*/
type MerkleNode struct {
	Hash  []byte
	Left  *MerkleNode
	Right *MerkleNode
	Key   []byte
	Value []byte
}

/*
MerkleTree maintains a hash tree over key-value pairs for O(log n) diff
detection. Rebuild must be called after Insert before GetDiff or VerifyProof.
*/
type MerkleTree struct {
	state    *errnie.State
	mu       sync.RWMutex
	Root     *MerkleNode
	leafMap  map[string]*MerkleNode
	parent   map[*MerkleNode]*MerkleNode
	modified bool
}

/*
NewMerkleTree allocates an empty tree. Call Insert then Rebuild before use.
*/
func NewMerkleTree() *MerkleTree {
	return &MerkleTree{
		state:   errnie.NewState("dmt/merkle"),
		leafMap: make(map[string]*MerkleNode),
		parent:  make(map[*MerkleNode]*MerkleNode),
	}
}

/*
Insert stores a key-value pair as a leaf. Copies key/value to avoid caller
aliasing. Rebuild required before GetDiff or VerifyProof.
*/
func (mt *MerkleTree) Insert(key, value []byte) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	key = append([]byte(nil), key...)
	value = append([]byte(nil), value...)

	leaf := &MerkleNode{
		Key:   key,
		Value: value,
		Hash:  mt.hashKV(key, value),
	}

	mt.leafMap[string(key)] = leaf
	mt.modified = true
}

/*
Rebuild reconstructs the tree from the current leaf set. No-op if unmodified.
*/
func (mt *MerkleTree) Rebuild() {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if !mt.modified {
		return
	}

	leaves := make([]*MerkleNode, 0, len(mt.leafMap))
	keys := make([]string, 0, len(mt.leafMap))

	mt.parent = make(map[*MerkleNode]*MerkleNode, len(mt.leafMap)*2)

	for k := range mt.leafMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		leaves = append(leaves, mt.leafMap[k])
	}

	mt.Root = mt.buildLevel(leaves)
	mt.modified = false
}

/*
buildLevel hashes pairs of nodes into parent nodes until one root remains.
*/
func (mt *MerkleTree) buildLevel(nodes []*MerkleNode) *MerkleNode {
	if len(nodes) == 0 {
		return nil
	}

	if len(nodes) == 1 {
		return nodes[0]
	}

	parents := make([]*MerkleNode, 0, (len(nodes)+1)/2)

	for i := 0; i < len(nodes); i += 2 {
		var right *MerkleNode

		left := nodes[i]

		if i+1 < len(nodes) {
			right = nodes[i+1]
		}

		var rightHash []byte

		if right != nil {
			rightHash = right.Hash
		}

		parent := &MerkleNode{
			Left:  left,
			Right: right,
			Hash:  mt.hashChildren(left.Hash, rightHash),
		}

		mt.parent[left] = parent

		if right != nil {
			mt.parent[right] = parent
		}

		parents = append(parents, parent)
	}

	return mt.buildLevel(parents)
}

/*
GetDiff returns keys that differ between this tree and other. Uses tree walk
when both have roots; otherwise falls back to full leaf comparison.
*/
func (mt *MerkleTree) GetDiff(other *MerkleTree) []DiffEntry {
	other.mu.RLock()
	defer other.mu.RUnlock()

	mt.mu.RLock()
	defer mt.mu.RUnlock()

	if mt.Root == nil || other.Root == nil {
		return mt.fullDiff(other)
	}

	diffs := make([]DiffEntry, 0)
	mt.diffNode(mt.Root, other.Root, other, &diffs)
	return diffs
}

/*
DiffEntry records one key-value pair that differs. Modified=true means the
key exists in both trees with different values; false means key exists only here.
*/
type DiffEntry struct {
	Key      []byte
	Value    []byte
	Modified bool
}

/*
diffNode walks two trees in parallel; when hashes differ, records leaf diffs.
*/
func (mt *MerkleTree) diffNode(a, b *MerkleNode, other *MerkleTree, diffs *[]DiffEntry) {
	if bytes.Equal(a.Hash, b.Hash) {
		return
	}

	if a.Key != nil {
		if otherLeaf, exists := other.leafMap[string(a.Key)]; exists {
			if !bytes.Equal(a.Value, otherLeaf.Value) {
				*diffs = append(*diffs, DiffEntry{
					Key:      a.Key,
					Value:    a.Value,
					Modified: true,
				})
			}
		} else {
			*diffs = append(*diffs, DiffEntry{
				Key:      a.Key,
				Value:    a.Value,
				Modified: false,
			})
		}

		return
	}

	if a.Left != nil && b.Left != nil {
		mt.diffNode(a.Left, b.Left, other, diffs)
	}

	if a.Right != nil && b.Right != nil {
		mt.diffNode(a.Right, b.Right, other, diffs)
	}
}

/*
fullDiff compares all leaves when roots are nil or trees are structurally
incompatible for a walk.
*/
func (mt *MerkleTree) fullDiff(other *MerkleTree) []DiffEntry {
	diffs := make([]DiffEntry, 0, len(mt.leafMap))

	for _, leaf := range mt.leafMap {
		if otherLeaf, exists := other.leafMap[string(leaf.Key)]; exists {
			if !bytes.Equal(leaf.Value, otherLeaf.Value) {
				diffs = append(diffs, DiffEntry{
					Key:      leaf.Key,
					Value:    leaf.Value,
					Modified: true,
				})
			}
		} else {
			diffs = append(diffs, DiffEntry{
				Key:      leaf.Key,
				Value:    leaf.Value,
				Modified: false,
			})
		}
	}

	return diffs
}

/*
Verify returns true if the key exists and its stored value matches.
*/
func (mt *MerkleTree) Verify(key, value []byte) bool {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	if leaf, exists := mt.leafMap[string(key)]; exists {
		return bytes.Equal(leaf.Value, value)
	}

	return false
}

/*
GetProof returns sibling hashes from leaf to root. Each element is prefixed
with proofPosLeft (sibling right) or proofPosRight (sibling left) so VerifyProof
knows hash order. When the current node is the left child and the parent has no
right sibling (odd leaf count at that level), the entry is proofPromoteNilRight
followed by proofNilRightPad so VerifyProof applies the same hashChildren(leaf,
nil) promotion used in Rebuild.
*/
func (mt *MerkleTree) GetProof(key []byte) ([][]byte, error) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	leaf, exists := mt.leafMap[string(key)]

	if !exists {
		errnie.GuardVoid(mt.state, func() error {
			return fmt.Errorf("key not found")
		})
		return nil, mt.state.Err()
	}

	proof := make([][]byte, 0)
	current := leaf

	for current != mt.Root {
		parent := mt.parent[current]

		if parent == nil {
			errnie.GuardVoid(mt.state, func() error {
				return fmt.Errorf("invalid tree structure")
			})
			return nil, mt.state.Err()
		}

		if parent.Left == current {
			if parent.Right != nil {
				entry := append([]byte{proofPosLeft}, parent.Right.Hash...)
				proof = append(proof, entry)
			} else {
				proof = append(proof, []byte{proofPromoteNilRight, proofNilRightPad})
			}
		} else {
			entry := append([]byte{proofPosRight}, parent.Left.Hash...)
			proof = append(proof, entry)
		}

		current = parent
	}

	return proof, nil
}

/*
VerifyProof recomputes the root from key/value and proof hashes. Returns true
if the result matches the stored root.
*/
func (mt *MerkleTree) VerifyProof(key, value []byte, proof [][]byte) bool {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	if mt.Root == nil {
		return false
	}

	hash := mt.hashKV(key, value)

	for _, entry := range proof {
		if len(entry) < 1 {
			return false
		}

		position := entry[0]

		if position == proofPromoteNilRight {
			if len(entry) != 2 || entry[1] != proofNilRightPad {
				return false
			}

			hash = mt.hashChildren(hash, nil)

			continue
		}

		if len(entry) <= 1 {
			return false
		}

		siblingHash := entry[1:]

		if position == proofPosLeft {
			hash = mt.hashChildren(hash, siblingHash)
		} else {
			hash = mt.hashChildren(siblingHash, hash)
		}
	}

	return bytes.Equal(hash, mt.Root.Hash)
}

/*
hashKV produces SHA-256(key || value) for leaf nodes without allocating a
hash.Hash state machine. The concatenation buffer is stack-eligible for
typical key-value sizes.
*/
func (mt *MerkleTree) hashKV(key, value []byte) []byte {
	buf := make([]byte, len(key)+len(value))
	copy(buf, key)
	copy(buf[len(key):], value)
	h := sha256.Sum256(buf)
	return h[:]
}

/*
hashChildren produces SHA-256(leftHash || rightHash). rightHash may be nil
for odd-count levels; in that case only leftHash is hashed (same as Rebuild
promotion). Accepts raw byte slices to avoid MerkleNode allocation in
VerifyProof.
*/
func (mt *MerkleTree) hashChildren(leftHash, rightHash []byte) []byte {
	buf := make([]byte, len(leftHash)+len(rightHash))
	copy(buf, leftHash)

	if len(rightHash) > 0 {
		copy(buf[len(leftHash):], rightHash)
	}

	h := sha256.Sum256(buf)
	return h[:]
}

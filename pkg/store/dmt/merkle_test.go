package dmt

import (
	"bytes"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewMerkleTree(t *testing.T) {
	Convey("Given a new Merkle tree", t, func() {
		mt := NewMerkleTree()

		Convey("Then it should be properly initialized", func() {
			So(mt, ShouldNotBeNil)
			So(mt.leafMap, ShouldNotBeNil)
			So(mt.Root, ShouldBeNil)
			So(mt.modified, ShouldBeFalse)
		})
	})
}

func TestMerkleTreeInsertAndRebuild(t *testing.T) {
	Convey("Given a Merkle tree", t, func() {
		mt := NewMerkleTree()

		Convey("When inserting key-value pairs", func() {
			mt.Insert([]byte("key1"), []byte("value1"))
			mt.Insert([]byte("key2"), []byte("value2"))
			mt.Insert([]byte("key3"), []byte("value3"))

			Convey("Then the tree should be marked as modified", func() {
				So(mt.modified, ShouldBeTrue)
			})

			Convey("When rebuilding the tree", func() {
				mt.Rebuild()

				Convey("Then the tree should have a valid root", func() {
					So(mt.Root, ShouldNotBeNil)
					So(mt.Root.Hash, ShouldNotBeNil)
					So(len(mt.Root.Hash), ShouldEqual, 32) // SHA-256 hash length
				})

				Convey("And the modified flag should be reset", func() {
					So(mt.modified, ShouldBeFalse)
				})
			})
		})
	})
}

func TestMerkleTreeDiff(t *testing.T) {
	Convey("Given two Merkle trees", t, func() {
		mt1 := NewMerkleTree()
		mt2 := NewMerkleTree()

		// Populate first tree
		mt1.Insert([]byte("key1"), []byte("value1"))
		mt1.Insert([]byte("key2"), []byte("value2"))
		mt1.Insert([]byte("key3"), []byte("value3"))
		mt1.Rebuild()

		// Populate second tree with some differences
		mt2.Insert([]byte("key1"), []byte("value1"))
		mt2.Insert([]byte("key2"), []byte("different"))
		mt2.Insert([]byte("key4"), []byte("value4"))
		mt2.Rebuild()

		Convey("When getting differences", func() {
			diffs := mt1.GetDiff(mt2)

			Convey("Then it should identify all differences", func() {
				So(len(diffs), ShouldEqual, 2) // key2 modified and key3 new

				// Verify the differences
				foundModified := false
				foundNew := false
				for _, diff := range diffs {
					if bytes.Equal(diff.Key, []byte("key2")) {
						foundModified = true
						So(diff.Modified, ShouldBeTrue)
					}
					if bytes.Equal(diff.Key, []byte("key3")) {
						foundNew = true
						So(diff.Modified, ShouldBeFalse)
					}
				}
				So(foundModified, ShouldBeTrue)
				So(foundNew, ShouldBeTrue)
			})
		})
	})
}

func TestMerkleTreeVerify(t *testing.T) {
	Convey("Given a Merkle tree with data", t, func() {
		mt := NewMerkleTree()
		mt.Insert([]byte("key1"), []byte("value1"))
		mt.Rebuild()

		Convey("When verifying existing data", func() {
			exists := mt.Verify([]byte("key1"), []byte("value1"))
			So(exists, ShouldBeTrue)
		})

		Convey("When verifying non-existent data", func() {
			exists := mt.Verify([]byte("nonexistent"), []byte("value"))
			So(exists, ShouldBeFalse)
		})

		Convey("When verifying with wrong value", func() {
			exists := mt.Verify([]byte("key1"), []byte("wrong"))
			So(exists, ShouldBeFalse)
		})
	})
}

func TestMerkleProof(t *testing.T) {
	Convey("Given a Merkle tree with multiple entries", t, func() {
		mt := NewMerkleTree()
		mt.Insert([]byte("key1"), []byte("value1"))
		mt.Insert([]byte("key2"), []byte("value2"))
		mt.Insert([]byte("key3"), []byte("value3"))
		mt.Insert([]byte("key4"), []byte("value4"))
		mt.Rebuild()

		Convey("When generating a proof", func() {
			proof, err := mt.GetProof([]byte("key2"))

			Convey("Then it should succeed", func() {
				So(err, ShouldBeNil)
				So(proof, ShouldNotBeNil)
				So(len(proof), ShouldBeGreaterThan, 0)
			})

			Convey("And the proof should be verifiable", func() {
				valid := mt.VerifyProof([]byte("key2"), []byte("value2"), proof)
				So(valid, ShouldBeTrue)
			})

			Convey("And the proof should fail for wrong values", func() {
				valid := mt.VerifyProof([]byte("key2"), []byte("wrong"), proof)
				So(valid, ShouldBeFalse)
			})
		})

		Convey("When generating a proof for non-existent key", func() {
			proof, err := mt.GetProof([]byte("nonexistent"))

			Convey("Then it should fail", func() {
				So(err, ShouldNotBeNil)
				So(proof, ShouldBeNil)
			})
		})
	})
}

func TestMerkleTreeDeterministic(t *testing.T) {
	Convey("Given two identical sets of data", t, func() {
		mt1 := NewMerkleTree()
		mt2 := NewMerkleTree()

		// Insert same data in different order
		mt1.Insert([]byte("key1"), []byte("value1"))
		mt1.Insert([]byte("key2"), []byte("value2"))
		mt1.Insert([]byte("key3"), []byte("value3"))

		mt2.Insert([]byte("key3"), []byte("value3"))
		mt2.Insert([]byte("key1"), []byte("value1"))
		mt2.Insert([]byte("key2"), []byte("value2"))

		Convey("When rebuilding both trees", func() {
			mt1.Rebuild()
			mt2.Rebuild()

			Convey("Then they should have identical root hashes", func() {
				So(bytes.Equal(mt1.Root.Hash, mt2.Root.Hash), ShouldBeTrue)
			})
		})
	})
}

/*
TestMerkleTreeRebuildNoOp checks that Rebuild does nothing when the tree is
already clean: the root node and its hash slice must keep the same pointers.
*/
func TestMerkleTreeRebuildNoOp(t *testing.T) {
	Convey("Given a rebuilt Merkle tree with two leaves", t, func() {
		mt := NewMerkleTree()
		mt.Insert([]byte("a"), []byte("1"))
		mt.Insert([]byte("b"), []byte("2"))
		mt.Rebuild()

		rootBefore := mt.Root
		hashBefore := mt.Root.Hash

		Convey("When Rebuild is called again without inserts", func() {
			mt.Rebuild()

			Convey("Then root and root hash pointers are unchanged", func() {
				So(mt.Root, ShouldEqual, rootBefore)
				So(mt.Root.Hash, ShouldEqual, hashBefore)
			})
		})
	})
}

/*
TestMerkleTreeSingleLeaf covers the degenerate single-leaf tree: proof is empty
and VerifyProof still matches the root hash.
*/
func TestMerkleTreeSingleLeaf(t *testing.T) {
	Convey("Given a Merkle tree with one entry after Rebuild", t, func() {
		mt := NewMerkleTree()
		key := []byte("only-key")
		value := []byte("only-value")
		mt.Insert(key, value)
		mt.Rebuild()

		Convey("Then the root is the leaf", func() {
			So(mt.Root, ShouldNotBeNil)
			So(mt.Root.Key, ShouldNotBeNil)
			So(bytes.Equal(mt.Root.Key, key), ShouldBeTrue)
			So(bytes.Equal(mt.Root.Value, value), ShouldBeTrue)
		})

		Convey("When GetProof is called", func() {
			proof, err := mt.GetProof(key)

			Convey("Then the proof is empty and valid", func() {
				So(err, ShouldBeNil)
				So(len(proof), ShouldEqual, 0)
			})
		})

		Convey("When VerifyProof is called with an empty proof", func() {
			ok := mt.VerifyProof(key, value, nil)

			Convey("Then verification succeeds", func() {
				So(ok, ShouldBeTrue)
			})
		})

		Convey("When VerifyProof is called with an empty slice proof", func() {
			ok := mt.VerifyProof(key, value, [][]byte{})

			Convey("Then verification succeeds", func() {
				So(ok, ShouldBeTrue)
			})
		})
	})
}

/*
TestMerkleTreeOddLeafCount exercises odd-sized levels (3 and 5 leaves) for
Verify and GetProof on every key.
*/
func TestMerkleTreeOddLeafCount(t *testing.T) {
	Convey("Given three leaves after Rebuild", t, func() {
		mt := NewMerkleTree()
		mt.Insert([]byte("k1"), []byte("v1"))
		mt.Insert([]byte("k2"), []byte("v2"))
		mt.Insert([]byte("k3"), []byte("v3"))
		mt.Rebuild()

		Convey("Then the root exists and every key verifies with a valid proof", func() {
			So(mt.Root, ShouldNotBeNil)
			for _, pair := range []struct {
				key, value []byte
			}{
				{[]byte("k1"), []byte("v1")},
				{[]byte("k2"), []byte("v2")},
				{[]byte("k3"), []byte("v3")},
			} {
				So(mt.Verify(pair.key, pair.value), ShouldBeTrue)
				proof, err := mt.GetProof(pair.key)
				So(err, ShouldBeNil)
				So(mt.VerifyProof(pair.key, pair.value, proof), ShouldBeTrue)
			}
		})
	})

	Convey("Given five leaves after Rebuild", t, func() {
		mt := NewMerkleTree()
		for i := 1; i <= 5; i++ {
			mt.Insert([]byte(fmt.Sprintf("key-%d", i)), []byte(fmt.Sprintf("val-%d", i)))
		}
		mt.Rebuild()

		Convey("Then the root exists and every key verifies with a valid proof", func() {
			So(mt.Root, ShouldNotBeNil)
			for i := 1; i <= 5; i++ {
				key := []byte(fmt.Sprintf("key-%d", i))
				value := []byte(fmt.Sprintf("val-%d", i))
				So(mt.Verify(key, value), ShouldBeTrue)
				proof, err := mt.GetProof(key)
				So(err, ShouldBeNil)
				So(mt.VerifyProof(key, value, proof), ShouldBeTrue)
			}
		})
	})
}

/*
TestMerkleTreeLargeTree stresses a hundred leaves with spot checks on proofs.
*/
func TestMerkleTreeLargeTree(t *testing.T) {
	Convey("Given 100 deterministic key-value pairs", t, func() {
		mt := NewMerkleTree()
		for i := 0; i < 100; i++ {
			mt.Insert(
				[]byte(fmt.Sprintf("k-%d", i)),
				[]byte(fmt.Sprintf("v-%d", i)),
			)
		}
		mt.Rebuild()

		Convey("Then every key verifies", func() {
			So(mt.Root, ShouldNotBeNil)
			for i := 0; i < 100; i++ {
				key := []byte(fmt.Sprintf("k-%d", i))
				value := []byte(fmt.Sprintf("v-%d", i))
				So(mt.Verify(key, value), ShouldBeTrue)
			}
		})

		Convey("When sampling ten keys with a fixed RNG", func() {
			rng := rand.New(rand.NewSource(42))
			picked := make(map[int]struct{})
			for len(picked) < 10 {
				picked[rng.Intn(100)] = struct{}{}
			}

			Convey("Then each sampled key has a verifiable proof", func() {
				for idx := range picked {
					key := []byte(fmt.Sprintf("k-%d", idx))
					value := []byte(fmt.Sprintf("v-%d", idx))
					proof, err := mt.GetProof(key)
					So(err, ShouldBeNil)
					So(mt.VerifyProof(key, value, proof), ShouldBeTrue)
				}
			})
		})
	})
}

/*
TestMerkleTreeUpdateExistingKey ensures Insert overwrites the leaf value used
by Verify after Rebuild.
*/
func TestMerkleTreeUpdateExistingKey(t *testing.T) {
	Convey("Given a key inserted twice with different values", t, func() {
		mt := NewMerkleTree()
		key := []byte("key1")
		mt.Insert(key, []byte("v1"))
		mt.Insert(key, []byte("v2"))
		mt.Rebuild()

		Convey("Then only the latest value verifies", func() {
			So(mt.Verify(key, []byte("v2")), ShouldBeTrue)
			So(mt.Verify(key, []byte("v1")), ShouldBeFalse)
		})
	})
}

/*
TestMerkleTreeDiffBothEmpty checks GetDiff with two untouched empty trees.
*/
func TestMerkleTreeDiffBothEmpty(t *testing.T) {
	Convey("Given two empty Merkle trees without Rebuild", t, func() {
		mt1 := NewMerkleTree()
		mt2 := NewMerkleTree()

		Convey("When GetDiff is called", func() {
			diffs := mt1.GetDiff(mt2)

			Convey("Then there are no differences", func() {
				So(len(diffs), ShouldEqual, 0)
			})
		})
	})
}

/*
TestMerkleTreeDiffOneEmpty ensures all leaves from the populated tree appear
in the diff against an empty peer.
*/
func TestMerkleTreeDiffOneEmpty(t *testing.T) {
	Convey("Given one populated tree and one empty tree", t, func() {
		populated := NewMerkleTree()
		empty := NewMerkleTree()
		populated.Insert([]byte("x"), []byte("1"))
		populated.Insert([]byte("y"), []byte("2"))
		populated.Insert([]byte("z"), []byte("3"))

		Convey("When GetDiff is called from populated to empty", func() {
			diffs := populated.GetDiff(empty)

			Convey("Then every populated key is reported as absent on the other side", func() {
				So(len(diffs), ShouldEqual, 3)
				seen := make(map[string]struct{})
				for _, entry := range diffs {
					So(entry.Modified, ShouldBeFalse)
					seen[string(entry.Key)] = struct{}{}
				}
				So(seen, ShouldContainKey, "x")
				So(seen, ShouldContainKey, "y")
				So(seen, ShouldContainKey, "z")
			})
		})
	})
}

/*
TestMerkleTreeVerifyProofNilRoot guards VerifyProof when no root exists.
*/
func TestMerkleTreeVerifyProofNilRoot(t *testing.T) {
	Convey("Given a Merkle tree with no root", t, func() {
		mt := NewMerkleTree()

		Convey("When VerifyProof is called", func() {
			ok := mt.VerifyProof([]byte("k"), []byte("v"), nil)

			Convey("Then verification fails", func() {
				So(ok, ShouldBeFalse)
			})
		})
	})
}

/*
TestMerkleTreeVerifyProofInvalidEntry rejects proof elements that are too
short to carry a sibling hash.
*/
func TestMerkleTreeVerifyProofInvalidEntry(t *testing.T) {
	Convey("Given a rebuilt Merkle tree", t, func() {
		mt := NewMerkleTree()
		mt.Insert([]byte("k"), []byte("v"))
		mt.Insert([]byte("k2"), []byte("v2"))
		mt.Rebuild()

		Convey("When VerifyProof uses a one-byte proof entry", func() {
			invalidProof := [][]byte{{0x00}}
			ok := mt.VerifyProof([]byte("k"), []byte("v"), invalidProof)

			Convey("Then verification fails", func() {
				So(ok, ShouldBeFalse)
			})
		})
	})
}

/*
TestMerkleTreeConcurrentInsert hammers Insert from many goroutines then Rebuild
once and verifies every key.
*/
func TestMerkleTreeConcurrentInsert(t *testing.T) {
	Convey("Given concurrent inserts from ten workers", t, func() {
		mt := NewMerkleTree()
		var wg sync.WaitGroup

		for worker := 0; worker < 10; worker++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					key := []byte(fmt.Sprintf("w%d-i%d", workerID, i))
					value := []byte(fmt.Sprintf("v%d-%d", workerID, i))
					mt.Insert(key, value)
				}
			}(worker)
		}

		wg.Wait()
		mt.Rebuild()

		Convey("Then all 1000 keys verify after a single Rebuild", func() {
			So(mt.Root, ShouldNotBeNil)
			for worker := 0; worker < 10; worker++ {
				for i := 0; i < 100; i++ {
					key := []byte(fmt.Sprintf("w%d-i%d", worker, i))
					value := []byte(fmt.Sprintf("v%d-%d", worker, i))
					So(mt.Verify(key, value), ShouldBeTrue)
				}
			}
		})
	})
}

/*
BenchmarkMerkleInsert measures Insert throughput with unique keys per loop.
*/
func BenchmarkMerkleInsert(b *testing.B) {
	mt := NewMerkleTree()
	b.ReportAllocs()
	n := 0

	for b.Loop() {
		mt.Insert(
			[]byte(fmt.Sprintf("bk-%d", n)),
			[]byte(fmt.Sprintf("bv-%d", n)),
		)
		n++
	}
}

/*
BenchmarkMerkleRebuild measures repeated Rebuild after a single key touch.
*/
func BenchmarkMerkleRebuild(b *testing.B) {
	mt := NewMerkleTree()
	for i := 0; i < 1000; i++ {
		mt.Insert(
			[]byte(fmt.Sprintf("k-%d", i)),
			[]byte(fmt.Sprintf("v-%d", i)),
		)
	}
	mt.Rebuild()
	touchKey := []byte("k-0")
	touchValue := []byte("v-0")
	b.ReportAllocs()

	for b.Loop() {
		mt.Insert(touchKey, touchValue)
		mt.Rebuild()
	}
}

/*
BenchmarkMerkleGetProof measures proof generation on a warm thousand-leaf tree.
*/
func BenchmarkMerkleGetProof(b *testing.B) {
	mt := NewMerkleTree()
	for i := 0; i < 1000; i++ {
		mt.Insert(
			[]byte(fmt.Sprintf("k-%d", i)),
			[]byte(fmt.Sprintf("v-%d", i)),
		)
	}
	mt.Rebuild()
	key := []byte("k-500")
	b.ReportAllocs()

	for b.Loop() {
		_, _ = mt.GetProof(key)
	}
}

/*
BenchmarkMerkleVerify measures leaf lookup verification on a warm tree.
*/
func BenchmarkMerkleVerify(b *testing.B) {
	mt := NewMerkleTree()
	for i := 0; i < 1000; i++ {
		mt.Insert(
			[]byte(fmt.Sprintf("k-%d", i)),
			[]byte(fmt.Sprintf("v-%d", i)),
		)
	}
	mt.Rebuild()
	key := []byte("k-500")
	value := []byte("v-500")
	b.ReportAllocs()

	for b.Loop() {
		_ = mt.Verify(key, value)
	}
}

/*
BenchmarkMerkleGetDiff measures diffing two 500-leaf trees sharing 250 keys.
*/
func BenchmarkMerkleGetDiff(b *testing.B) {
	mtA := NewMerkleTree()
	mtB := NewMerkleTree()

	for i := 0; i < 250; i++ {
		key := []byte(fmt.Sprintf("shared-%d", i))
		value := []byte(fmt.Sprintf("sv-%d", i))
		mtA.Insert(key, value)
		mtB.Insert(append([]byte(nil), key...), append([]byte(nil), value...))
	}

	for i := 0; i < 250; i++ {
		mtA.Insert(
			[]byte(fmt.Sprintf("a-%d", i)),
			[]byte(fmt.Sprintf("av-%d", i)),
		)
	}

	for i := 0; i < 250; i++ {
		mtB.Insert(
			[]byte(fmt.Sprintf("b-%d", i)),
			[]byte(fmt.Sprintf("bv-%d", i)),
		)
	}

	mtA.Rebuild()
	mtB.Rebuild()
	b.ReportAllocs()

	for b.Loop() {
		_ = mtA.GetDiff(mtB)
	}
}

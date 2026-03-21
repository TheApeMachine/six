package dmt

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewTree(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When a new tree is created", func() {
			So(tree, ShouldNotBeNil)
		})
	})
}

func TestSeek(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When a seek is performed", func() {
			tree.Insert([]byte("test"), []byte("test"))
			value, ok := tree.Seek([]byte("test"))
			So(ok, ShouldBeTrue)
			So(value, ShouldResemble, []byte("test"))
		})
	})
}

func TestInsert(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When an insert is performed", func() {
			newTree, ok := tree.Insert([]byte("test"), []byte("test"))
			So(ok, ShouldBeTrue)
			So(newTree, ShouldNotBeNil)

			// Verify the insert worked
			value, exists := newTree.Get([]byte("test"))
			So(exists, ShouldBeTrue)
			So(value, ShouldResemble, []byte("test"))
		})
	})
}

func TestGet(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When a get is performed", func() {
			tree.Insert([]byte("test"), []byte("test"))
			value, ok := tree.Get([]byte("test"))
			So(ok, ShouldBeTrue)
			So(value, ShouldResemble, []byte("test"))
		})
	})
}

func TestAVG(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When a avg is performed", func() {
			tree.Insert([]byte("test"), []byte("test"))
			tree.Get([]byte("test"))
			avg := tree.AVG()
			So(avg, ShouldBeGreaterThan, 0)
		})
	})
}

func TestClose(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When a close is performed", func() {
			err := tree.Close()
			So(err, ShouldBeNil)
		})
	})
}

func TestUpdateTerm(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When a update term is performed", func() {
			tree.UpdateTerm(1)
			So(tree.term, ShouldEqual, 1)
		})
	})
}

func TestGetLogState(t *testing.T) {
	Convey("Given a new tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)

		Convey("When a get log state is performed", func() {
			term, index := tree.GetLogState()
			So(term, ShouldEqual, 0)
			So(index, ShouldEqual, 0)
		})
	})
}

func TestTreeWithPersistence(t *testing.T) {
	Convey("Given a temporary directory", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		Convey("When creating a tree with persistence", func() {
			tree, err := NewTree(tmpDir)
			So(err, ShouldBeNil)
			defer tree.Close()

			Convey("Then the persistence store should be initialized", func() {
				So(tree.persist, ShouldNotBeNil)
			})

			Convey("And when inserting data", func() {
				newTree, ok := tree.Insert([]byte("test-key"), []byte("test-value"))
				So(ok, ShouldBeTrue)
				So(newTree, ShouldNotBeNil)

				Convey("The data should be persisted", func() {
					tree.Close()

					tree2, err := NewTree(tmpDir)
					So(err, ShouldBeNil)
					defer tree2.Close()

					// Verify term and index were loaded
					term, index := tree2.GetLogState()
					So(term, ShouldEqual, tree.term)
					So(index, ShouldEqual, tree.logIndex)

					// Verify data was recovered
					value, exists := tree2.Get([]byte("test-key"))
					So(exists, ShouldBeTrue)
					So(value, ShouldResemble, []byte("test-value"))
				})
			})
		})
	})
}

func TestTreeStateRecovery(t *testing.T) {
	Convey("Given a tree with existing WAL", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		// Create and populate first tree
		tree1, err := NewTree(tmpDir)
		So(err, ShouldBeNil)

		entries := []struct {
			key   string
			value string
			term  uint64
		}{
			{"key1", "value1", 1},
			{"key2", "value2", 1},
			{"key3", "value3", 2},
		}

		for _, e := range entries {
			tree1.UpdateTerm(e.term)
			tree1, _ = tree1.Insert([]byte(e.key), []byte(e.value))
		}
		tree1.Close()

		Convey("When creating a new tree instance", func() {
			tree2, err := NewTree(tmpDir)
			So(err, ShouldBeNil)
			defer tree2.Close()

			Convey("Then it should recover the correct state", func() {
				term, index := tree2.GetLogState()
				So(term, ShouldEqual, entries[len(entries)-1].term)
				So(index, ShouldEqual, uint64(len(entries)))

				Convey("And all data should be accessible", func() {
					for _, e := range entries {
						value, exists := tree2.Get([]byte(e.key))
						So(exists, ShouldBeTrue)
						So(value, ShouldResemble, []byte(e.value))
					}
				})
			})
		})
	})
}

func TestTreeTermUpdate(t *testing.T) {
	Convey("Given a persistent tree", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		tree, err := NewTree(tmpDir)
		So(err, ShouldBeNil)
		defer tree.Close()

		Convey("When updating the term", func() {
			tree.UpdateTerm(5)

			Convey("Then the term should be persisted", func() {
				term, _ := tree.GetLogState()
				So(term, ShouldEqual, uint64(5))

				// Verify term survives restart
				tree.Close()
				newTree, err := NewTree(tmpDir)
				So(err, ShouldBeNil)
				defer newTree.Close()

				term, _ = newTree.GetLogState()
				So(term, ShouldEqual, uint64(5))
			})
		})
	})
}

func BenchmarkTreeInsert(b *testing.B) {
	tree, err := NewTree("")
	if err != nil {
		b.Fatalf("failed to create tree: %v", err)
	}
	defer tree.Close()

	b.ReportAllocs()

	index := 0
	for b.Loop() {
		key := []byte(fmt.Sprintf("bench-key-%d", index))
		value := []byte(fmt.Sprintf("bench-value-%d", index))
		tree.Insert(key, value)
		index++
	}
}

func BenchmarkTreeGet(b *testing.B) {
	tree, err := NewTree("")
	if err != nil {
		b.Fatalf("failed to create tree: %v", err)
	}
	defer tree.Close()

	const seedCount = 4096
	for i := 0; i < seedCount; i++ {
		key := []byte(fmt.Sprintf("seed-key-%d", i))
		value := []byte(fmt.Sprintf("seed-value-%d", i))
		tree.Insert(key, value)
	}

	index := 0
	b.ReportAllocs()

	for b.Loop() {
		key := []byte(fmt.Sprintf("seed-key-%d", index%seedCount))
		value, ok := tree.Get(key)
		if !ok || len(value) == 0 {
			b.Fatalf("missing key: %s", key)
		}
		index++
	}
}

/*
TestTreeGetMiss verifies Get on a missing key returns nil and false.
*/
func TestTreeGetMiss(t *testing.T) {
	Convey("Given a tree without the key", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		Convey("When Get is called for a non-existent key", func() {
			value, ok := tree.Get([]byte("no-such-key"))
			So(ok, ShouldBeFalse)
			So(value, ShouldBeNil)
		})
	})
}

/*
TestTreeSeekMiss verifies Seek past the last key returns nil and false.
*/
func TestTreeSeekMiss(t *testing.T) {
	Convey("Given a tree with bounded keys", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		tree.Insert([]byte("alpha"), []byte("a"))
		tree.Insert([]byte("beta"), []byte("b"))

		Convey("When Seek uses a key larger than any stored key", func() {
			value, ok := tree.Seek([]byte("zzz"))
			So(ok, ShouldBeFalse)
			So(value, ShouldBeNil)
		})
	})
}

/*
TestTreeSeekPrefix verifies SeekLowerBound-style prefix behavior on shared prefixes.
*/
func TestTreeSeekPrefix(t *testing.T) {
	Convey("Given keys sharing prefixes", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		tree.Insert([]byte("abc"), []byte("v-abc"))
		tree.Insert([]byte("abd"), []byte("v-abd"))
		tree.Insert([]byte("xyz"), []byte("v-xyz"))

		Convey("When Seek targets prefix ab", func() {
			value, ok := tree.Seek([]byte("ab"))
			So(ok, ShouldBeTrue)
			So(value, ShouldResemble, []byte("v-abc"))
		})

		Convey("When Seek targets prefix xy", func() {
			value, ok := tree.Seek([]byte("xy"))
			So(ok, ShouldBeTrue)
			So(value, ShouldResemble, []byte("v-xyz"))
		})
	})
}

/*
TestTreeInsertDuplicate verifies updating an existing key returns updated=true and replaces the value.
*/
func TestTreeInsertDuplicate(t *testing.T) {
	Convey("Given a tree with an existing key", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		tree, ok := tree.Insert([]byte("same"), []byte("first"))
		So(ok, ShouldBeTrue)

		Convey("When inserting the same key with a different value", func() {
			newTree, updated := tree.Insert([]byte("same"), []byte("second"))
			So(updated, ShouldBeTrue)
			So(newTree, ShouldNotBeNil)

			value, exists := newTree.Get([]byte("same"))
			So(exists, ShouldBeTrue)
			So(value, ShouldResemble, []byte("second"))
		})
	})
}

/*
TestTreeInsertDoesNotAlias verifies inserted key and value slices are copied internally.
*/
func TestTreeInsertDoesNotAlias(t *testing.T) {
	Convey("Given mutable key and value slices", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		key := []byte{'k', 'e', 'y'}
		value := []byte{'v', 'a', 'l'}

		tree.Insert(key, value)

		key[0] = 'X'
		value[0] = 'Y'

		Convey("When Get runs after mutating caller slices", func() {
			got, ok := tree.Get([]byte("key"))
			So(ok, ShouldBeTrue)
			So(got, ShouldResemble, []byte("val"))
		})
	})
}

/*
TestTreeAVGEmptyRing verifies AVG is zero before any timed operation records samples.
*/
func TestTreeAVGEmptyRing(t *testing.T) {
	Convey("Given a fresh tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		Convey("When AVG is read with no operations", func() {
			So(tree.AVG(), ShouldEqual, 0)
		})
	})
}

/*
TestTreeConcurrentAccess verifies many parallel inserts on one tree remain readable.
*/
func TestTreeConcurrentAccess(t *testing.T) {
	Convey("Given a shared tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		const goroutines = 10
		const perGoroutine = 100

		var wg sync.WaitGroup

		for g := 0; g < goroutines; g++ {
			wg.Add(1)

			go func(group int) {
				defer wg.Done()

				for i := 0; i < perGoroutine; i++ {
					key := fmt.Sprintf("g%02d-k%03d", group, i)
					tree.Insert([]byte(key), []byte("v"))
				}
			}(g)
		}

		wg.Wait()

		Convey("When every inserted key is read back", func() {
			for g := 0; g < goroutines; g++ {
				for i := 0; i < perGoroutine; i++ {
					key := fmt.Sprintf("g%02d-k%03d", g, i)
					val, ok := tree.Get([]byte(key))
					So(ok, ShouldBeTrue)
					So(val, ShouldResemble, []byte("v"))
				}
			}
		})
	})
}

/*
TestTreeConcurrentReadWrite runs a writer alongside readers then asserts full retrieval.
*/
func TestTreeConcurrentReadWrite(t *testing.T) {
	Convey("Given a shared tree and concurrent readers", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		const keyCount = 500

		var wg sync.WaitGroup

		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < keyCount; i++ {
				key := fmt.Sprintf("rw-key-%d", i)
				tree.Insert([]byte(key), []byte("payload"))
			}
		}()

		for r := 0; r < 5; r++ {
			wg.Add(1)

			go func() {
				defer wg.Done()

				for round := 0; round < 2000; round++ {
					idx := round % keyCount
					key := fmt.Sprintf("rw-key-%d", idx)
					tree.Get([]byte(key))
				}
			}()
		}

		wg.Wait()

		Convey("When all writer keys are verified after completion", func() {
			for i := 0; i < keyCount; i++ {
				key := fmt.Sprintf("rw-key-%d", i)
				val, ok := tree.Get([]byte(key))
				So(ok, ShouldBeTrue)
				So(val, ShouldResemble, []byte("payload"))
			}
		})
	})
}

/*
TestTreeInsertMany stresses bulk insert and full read-back.
*/
func TestTreeInsertMany(t *testing.T) {
	Convey("Given a tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		const n = 10000

		for i := 0; i < n; i++ {
			key := fmt.Sprintf("many-%d", i)
			tree.Insert([]byte(key), []byte(key))
		}

		Convey("When each inserted key is retrieved", func() {
			for i := 0; i < n; i++ {
				key := fmt.Sprintf("many-%d", i)
				val, ok := tree.Get([]byte(key))
				So(ok, ShouldBeTrue)
				So(val, ShouldResemble, []byte(key))
			}
		})
	})
}

/*
TestTreeLogIndexIncrement verifies in-memory inserts advance log index without persistence.
*/
func TestTreeLogIndexIncrement(t *testing.T) {
	Convey("Given an ephemeral tree", t, func() {
		tree, err := NewTree("")
		So(err, ShouldBeNil)
		defer tree.Close()

		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("log-%d", i)
			var updated bool

			tree, updated = tree.Insert([]byte(key), []byte("x"))
			So(updated, ShouldBeTrue)
		}

		Convey("When log state is read", func() {
			term, index := tree.GetLogState()
			So(term, ShouldEqual, 0)
			So(index, ShouldEqual, 5)
		})
	})
}

/*
TestTreeCloseWithPersistence verifies close and reopen preserves stored entries.
*/
func TestTreeCloseWithPersistence(t *testing.T) {
	Convey("Given a persistent tree directory", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "tree-close-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		tree, err := NewTree(tmpDir)
		So(err, ShouldBeNil)

		tree.Insert([]byte("persist-key"), []byte("persist-val"))
		closeErr := tree.Close()
		So(closeErr, ShouldBeNil)

		Convey("When a new tree opens the same directory", func() {
			tree2, errOpen := NewTree(tmpDir)
			So(errOpen, ShouldBeNil)
			defer tree2.Close()

			val, ok := tree2.Get([]byte("persist-key"))
			So(ok, ShouldBeTrue)
			So(val, ShouldResemble, []byte("persist-val"))
		})
	})
}

/*
BenchmarkTreeSeek measures Seek throughput over a populated tree.
*/
func BenchmarkTreeSeek(b *testing.B) {
	tree, err := NewTree("")
	if err != nil {
		b.Fatalf("failed to create tree: %v", err)
	}
	defer tree.Close()

	const seedCount = 4096

	for i := 0; i < seedCount; i++ {
		key := []byte(fmt.Sprintf("seek-seed-%d", i))
		value := []byte(fmt.Sprintf("seek-val-%d", i))
		tree.Insert(key, value)
	}

	index := 0

	b.ReportAllocs()

	for b.Loop() {
		seekKey := []byte(fmt.Sprintf("seek-seed-%d", index%seedCount))
		val, ok := tree.Seek(seekKey)

		if !ok || len(val) == 0 {
			b.Fatalf("seek miss: %s", seekKey)
		}

		index++
	}
}

/*
BenchmarkTreeInsertWithPersistence measures insert cost with WAL-backed storage.
*/
func BenchmarkTreeInsertWithPersistence(b *testing.B) {
	tmpDir := filepath.Join(os.TempDir(), "tree-bench-persist-"+time.Now().Format("20060102150405"))
	b.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	tree, err := NewTree(tmpDir)
	if err != nil {
		b.Fatalf("failed to create tree: %v", err)
	}

	b.Cleanup(func() {
		_ = tree.Close()
	})

	index := 0

	b.ReportAllocs()

	for b.Loop() {
		key := []byte(fmt.Sprintf("pbench-key-%d", index))
		value := []byte(fmt.Sprintf("pbench-val-%d", index))
		tree.Insert(key, value)
		index++
	}
}

/*
BenchmarkTreeAVG measures AVG aggregation after operations have populated the perf ring.
*/
func BenchmarkTreeAVG(b *testing.B) {
	tree, err := NewTree("")
	if err != nil {
		b.Fatalf("failed to create tree: %v", err)
	}
	defer tree.Close()

	for i := 0; i < 64; i++ {
		key := []byte(fmt.Sprintf("avg-warm-%d", i))
		tree.Insert(key, key)
		tree.Get(key)
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = tree.AVG()
	}
}

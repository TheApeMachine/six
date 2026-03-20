package dmt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
forestBenchmarkSink retains side effects from forest benchmarks so the compiler
cannot eliminate Iterate bodies as dead code.
*/
var forestBenchmarkSink int

func TestNewForest(t *testing.T) {
	Convey("Given a forest configuration", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		config := ForestConfig{
			PersistDir: tmpDir,
		}

		Convey("When creating a new forest", func() {
			forest, err := NewForest(config)
			So(err, ShouldBeNil)
			defer forest.Close()

			Convey("Then it should be properly initialized", func() {
				So(forest, ShouldNotBeNil)
				So(forest.trees, ShouldNotBeNil)
				So(forest.updates, ShouldNotBeNil)
				So(forest.ctx, ShouldNotBeNil)
				So(forest.cancel, ShouldNotBeNil)
			})

			Convey("And it should have one initial tree", func() {
				So(len(forest.trees), ShouldEqual, 1)
				So(forest.trees[0], ShouldNotBeNil)
			})
		})
	})
}

func TestForestUsesProvidedPool(t *testing.T) {
	Convey("Given a forest configuration with a worker pool", t, func() {
		workerPool := pool.New(context.Background(), 1, 8, &pool.Config{})
		defer workerPool.Close()

		forest, err := NewForest(ForestConfig{
			Pool: workerPool,
		})
		So(err, ShouldBeNil)
		defer forest.Close()

		Convey("Then the forest should reuse the provided pool", func() {
			So(forest.pool, ShouldEqual, workerPool)
			So(forest.owned, ShouldBeFalse)
		})
	})
}

func TestForestOperations(t *testing.T) {
	Convey("Given a new forest", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		forest, err := NewForest(ForestConfig{PersistDir: tmpDir})
		So(err, ShouldBeNil)
		defer forest.Close()

		Convey("When performing insert operations", func() {
			forest.Insert([]byte("key1"), []byte("value1"))
			forest.Insert([]byte("key2"), []byte("value2"))

			Convey("Then the data should be retrievable", func() {
				value1, exists := forest.Get([]byte("key1"))
				So(exists, ShouldBeTrue)
				So(value1, ShouldResemble, []byte("value1"))

				value2, exists := forest.Get([]byte("key2"))
				So(exists, ShouldBeTrue)
				So(value2, ShouldResemble, []byte("value2"))
			})

			Convey("And seek operations should work", func() {
				value, exists := forest.Seek([]byte("key"))
				So(exists, ShouldBeTrue)
				So(value, ShouldResemble, []byte("value1")) // Should find first key alphabetically
			})
		})
	})
}

func TestForestSynchronization(t *testing.T) {
	Convey("Given a forest with multiple trees", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		forest, err := NewForest(ForestConfig{PersistDir: tmpDir})
		So(err, ShouldBeNil)
		defer forest.Close()

		// Add a second tree
		tree2, err := NewTree("")
		So(err, ShouldBeNil)
		forest.AddTree(tree2)

		Convey("When inserting data", func() {
			forest.Insert([]byte("sync-key"), []byte("sync-value"))

			// Wait for sync to complete
			time.Sleep(100 * time.Millisecond)

			Convey("Then all trees should have the data", func() {
				for _, tree := range forest.trees {
					value, exists := tree.Get([]byte("sync-key"))
					So(exists, ShouldBeTrue)
					So(value, ShouldResemble, []byte("sync-value"))
				}
			})
		})
	})
}

func TestForestPerformance(t *testing.T) {
	Convey("Given a forest with multiple trees", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		// Add trees with different simulated performance characteristics
		tree1, err := NewTree("")
		So(err, ShouldBeNil)
		tree2, err := NewTree("")
		So(err, ShouldBeNil)

		forest.AddTree(tree1)
		forest.AddTree(tree2)

		Convey("When getting the fastest tree", func() {
			fastestTree := forest.getFastestTree()

			Convey("Then it should return a valid tree", func() {
				So(fastestTree, ShouldNotBeNil)
				So(fastestTree.AVG(), ShouldBeGreaterThanOrEqualTo, 0)
			})
		})
	})
}

func TestForestNetworking(t *testing.T) {
	Convey("Given a forest with network configuration", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		config := ForestConfig{
			PersistDir: tmpDir,
			Network: &NetworkConfig{
				ListenAddr: ":0", // Use random port
				NodeID:     "test-node",
			},
		}

		forest, err := NewForest(config)
		So(err, ShouldBeNil)
		defer forest.Close()

		Convey("Then the network node should be initialized", func() {
			So(forest.network, ShouldNotBeNil)
			So(forest.network.config.NodeID, ShouldEqual, "test-node")
		})

		Convey("When inserting data with networking enabled", func() {
			forest.Insert([]byte("network-key"), []byte("network-value"))

			Convey("Then the network node should broadcast the insert", func() {
				// Verify local insertion worked
				value, exists := forest.Get([]byte("network-key"))
				So(exists, ShouldBeTrue)
				So(value, ShouldResemble, []byte("network-value"))

				// Network metrics should be updated
				metrics := forest.network.GetMetrics()
				networkStats := metrics["network"].(map[string]interface{})
				So(networkStats["bytes_tx"], ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestForestClose(t *testing.T) {
	Convey("Given a forest with multiple components", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		config := ForestConfig{
			PersistDir: tmpDir,
			Network: &NetworkConfig{
				ListenAddr: ":0",
				NodeID:     "test-node",
			},
		}

		forest, err := NewForest(config)
		So(err, ShouldBeNil)

		// Add additional tree
		tree2, err := NewTree("")
		So(err, ShouldBeNil)
		forest.AddTree(tree2)

		Convey("When closing the forest", func() {
			forest.Close()

			Convey("Then the context should be cancelled", func() {
				select {
				case <-forest.ctx.Done():
					// Context was cancelled as expected
					So(true, ShouldBeTrue)
				default:
					So(false, ShouldBeTrue, "Context was not cancelled")
				}
			})
		})
	})
}

func TestForestIterate(t *testing.T) {
	Convey("Given a forest with five distinct keys", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		forest, err := NewForest(ForestConfig{PersistDir: tmpDir})
		So(err, ShouldBeNil)
		defer forest.Close()

		want := map[string]string{
			"alpha": "1",
			"beta":  "2",
			"gamma": "3",
			"delta": "4",
			"epsilon": "5",
		}

		for keyString, valueString := range want {
			forest.Insert([]byte(keyString), []byte(valueString))
		}

		Convey("When iterating the fastest tree", func() {
			seen := make(map[string]string)

			forest.Iterate(func(key []byte, value []byte) bool {
				seen[string(key)] = string(value)

				return true
			})

			Convey("Then every inserted pair should be visited exactly once", func() {
				So(len(seen), ShouldEqual, len(want))

				for keyString, valueString := range want {
					So(seen[keyString], ShouldEqual, valueString)
				}
			})
		})
	})
}

func TestForestIterateEarlyStop(t *testing.T) {
	Convey("Given a forest with ten keys", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		for index := 0; index < 10; index++ {
			key := fmt.Appendf(nil, "early-%02d", index)
			value := fmt.Appendf(nil, "v-%02d", index)

			forest.Insert(key, value)
		}

		Convey("When iteration stops after three entries", func() {
			var visitCount int

			forest.Iterate(func(key []byte, value []byte) bool {
				visitCount++

				return visitCount < 3
			})

			Convey("Then only three entries should have been visited", func() {
				So(visitCount, ShouldEqual, 3)
			})
		})
	})
}

func TestForestGetMiss(t *testing.T) {
	Convey("Given a forest without a matching key", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		forest.Insert([]byte("present"), []byte("yes"))

		Convey("When getting a missing key", func() {
			value, exists := forest.Get([]byte("absent-key"))

			Convey("Then the lookup should report a miss", func() {
				So(exists, ShouldBeFalse)
				So(value, ShouldBeNil)
			})
		})
	})
}

func TestForestSeekMiss(t *testing.T) {
	Convey("Given a forest with keys below the seek bound", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		forest.Insert([]byte("a"), []byte("va"))
		forest.Insert([]byte("b"), []byte("vb"))
		forest.Insert([]byte("c"), []byte("vc"))

		Convey("When seeking past the last key", func() {
			value, exists := forest.Seek([]byte("z"))

			Convey("Then no successor should be reported", func() {
				So(exists, ShouldBeFalse)
				So(value, ShouldBeNil)
			})
		})
	})
}

func TestForestEmptyGet(t *testing.T) {
	Convey("Given a freshly created forest with no inserts", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		Convey("When reading any key", func() {
			value, exists := forest.Get([]byte("anything"))

			Convey("Then the forest should report a miss", func() {
				So(exists, ShouldBeFalse)
				So(value, ShouldBeNil)
			})
		})
	})
}

func TestForestMultiTreeConsistency(t *testing.T) {
	Convey("Given a forest with three trees", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		forest, err := NewForest(ForestConfig{PersistDir: tmpDir})
		So(err, ShouldBeNil)
		defer forest.Close()

		extraTreeA, err := NewTree("")
		So(err, ShouldBeNil)

		extraTreeB, err := NewTree("")
		So(err, ShouldBeNil)

		forest.AddTree(extraTreeA)
		forest.AddTree(extraTreeB)

		So(len(forest.trees), ShouldEqual, 3)

		keys := make([][]byte, 50)
		for index := 0; index < 50; index++ {
			keys[index] = fmt.Appendf(nil, "multi-%02d", index)
			forest.Insert(keys[index], []byte("payload"))
		}

		Convey("When each tree is queried directly", func() {
			for _, tree := range forest.trees {
				for _, key := range keys {
					value, exists := tree.Get(key)

					So(exists, ShouldBeTrue)
					So(value, ShouldResemble, []byte("payload"))
				}
			}
		})
	})
}

func TestForestConcurrentInsertGet(t *testing.T) {
	Convey("Given a forest with pre-seeded data", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		forest.Insert([]byte("pre-seed"), []byte("seed-value"))

		var inserted [][]byte
		var insertMu sync.Mutex
		var waitGroup sync.WaitGroup

		for workerIndex := 0; workerIndex < 5; workerIndex++ {
			waitGroup.Add(1)

			go func(workerID int) {
				defer waitGroup.Done()

				for sequence := 0; sequence < 20; sequence++ {
					key := fmt.Appendf(nil, "conc-%d-%04d", workerID, sequence)

					forest.Insert(key, []byte("x"))

					insertMu.Lock()
					inserted = append(inserted, append([]byte(nil), key...))
					insertMu.Unlock()
				}
			}(workerIndex)
		}

		for readerIndex := 0; readerIndex < 5; readerIndex++ {
			waitGroup.Add(1)

			go func() {
				defer waitGroup.Done()

				for round := 0; round < 500; round++ {
					forest.Get([]byte("pre-seed"))

					insertMu.Lock()
					snapshot := append([][]byte(nil), inserted...)
					insertMu.Unlock()

					for _, key := range snapshot {
						forest.Get(key)
					}
				}
			}()
		}

		waitGroup.Wait()

		Convey("Then every concurrently inserted key should be readable", func() {
			insertMu.Lock()
			finalKeys := append([][]byte(nil), inserted...)
			insertMu.Unlock()

			So(len(finalKeys), ShouldEqual, 100)

			for _, key := range finalKeys {
				value, exists := forest.Get(key)

				So(exists, ShouldBeTrue)
				So(value, ShouldResemble, []byte("x"))
			}
		})
	})
}

func TestForestSynchronizeTrees(t *testing.T) {
	Convey("Given a forest whose second tree lags the first", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		forest, err := NewForest(ForestConfig{PersistDir: tmpDir})
		So(err, ShouldBeNil)
		defer forest.Close()

		secondTree, err := NewTree("")
		So(err, ShouldBeNil)

		forest.AddTree(secondTree)

		/*
			Let the sync loop drain the AddTree signal while both trees are still
			empty so a later direct insert into trees[0] is not merged into trees[1]
			before we assert the lagging state.
		*/
		time.Sleep(250 * time.Millisecond)

		key := []byte("direct-only")
		value := []byte("unsynced-path")

		forest.trees[0].Insert(key, value)

		before, existsBefore := forest.trees[1].Get(key)
		So(existsBefore, ShouldBeFalse)
		So(before, ShouldBeNil)

		Convey("When synchronizeTrees merges from the reference tree", func() {
			forest.synchronizeTrees()

			after, existsAfter := forest.trees[1].Get(key)

			Convey("Then the lagging tree should contain the missing entry", func() {
				So(existsAfter, ShouldBeTrue)
				So(after, ShouldResemble, value)
			})
		})
	})
}

/*
BenchmarkForestInsert measures steady-state Insert on a single key.
*/
func BenchmarkForestInsert(b *testing.B) {
	forest, err := NewForest(ForestConfig{})
	if err != nil {
		b.Fatal(err)
	}

	defer forest.Close()

	key := []byte("bench-insert-key")
	value := []byte("bench-insert-value")

	b.ReportAllocs()

	for b.Loop() {
		forest.Insert(key, value)
	}
}

/*
BenchmarkForestGet measures Get against a forest preloaded with 4096 keys.
*/
func BenchmarkForestGet(b *testing.B) {
	forest, err := NewForest(ForestConfig{})
	if err != nil {
		b.Fatal(err)
	}

	defer forest.Close()

	lookupKey := []byte("bench-get-2048")

	for index := 0; index < 4096; index++ {
		key := fmt.Appendf(nil, "bench-get-%d", index)
		forest.Insert(key, key)
	}

	b.ReportAllocs()

	for b.Loop() {
		forest.Get(lookupKey)
	}
}

/*
BenchmarkForestSeek measures Seek against a forest preloaded with 4096 keys.
*/
func BenchmarkForestSeek(b *testing.B) {
	forest, err := NewForest(ForestConfig{})
	if err != nil {
		b.Fatal(err)
	}

	defer forest.Close()

	seekKey := []byte("bench-seek-2048")

	for index := 0; index < 4096; index++ {
		key := fmt.Appendf(nil, "bench-seek-%d", index)
		forest.Insert(key, key)
	}

	b.ReportAllocs()

	for b.Loop() {
		forest.Seek(seekKey)
	}
}

/*
BenchmarkForestIterate walks one thousand keys per iteration on the fastest tree.
*/
func BenchmarkForestIterate(b *testing.B) {
	forest, err := NewForest(ForestConfig{})
	if err != nil {
		b.Fatal(err)
	}

	defer forest.Close()

	for index := 0; index < 1000; index++ {
		key := fmt.Appendf(nil, "bench-iter-%d", index)
		forest.Insert(key, key)
	}

	b.ReportAllocs()

	for b.Loop() {
		var visitCount int

		forest.Iterate(func(key []byte, value []byte) bool {
			visitCount++

			return true
		})

		forestBenchmarkSink += visitCount
	}
}

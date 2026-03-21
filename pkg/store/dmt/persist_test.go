package dmt

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewPersistentStore(t *testing.T) {
	Convey("Given a temporary directory", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		Convey("When creating a new persistent store", func() {
			store, err := NewPersistentStore(tmpDir)
			So(err, ShouldBeNil)
			defer store.Close()

			Convey("Then the store should be properly initialized", func() {
				So(store, ShouldNotBeNil)
				So(store.walFile, ShouldNotBeNil)
				So(store.walWriter, ShouldNotBeNil)
				So(store.snapCount, ShouldEqual, 1000)
			})

			Convey("And the WAL file should exist", func() {
				_, err := os.Stat(filepath.Join(tmpDir, "wal.log"))
				So(err, ShouldBeNil)
			})
		})
	})
}

func TestLogInsert(t *testing.T) {
	Convey("Given a new persistent store", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)
		defer store.Close()

		Convey("When logging an insert operation", func() {
			key := []byte("test-key")
			value := []byte("test-value")
			term := uint64(1)
			index := uint64(1)

			err := store.LogInsert(key, value, term, index)
			So(err, ShouldBeNil)

			Convey("Then the state should be updated", func() {
				lastTerm, lastIndex := store.GetLastState()
				So(lastTerm, ShouldEqual, term)
				So(lastIndex, ShouldEqual, index)
			})
		})
	})
}

func TestLoadLastState(t *testing.T) {
	Convey("Given a persistent store with existing entries", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)
		defer store.Close()

		// Add some entries
		entries := []struct {
			key   []byte
			value []byte
			term  uint64
			index uint64
		}{
			{[]byte("key1"), []byte("value1"), 1, 1},
			{[]byte("key2"), []byte("value2"), 1, 2},
			{[]byte("key3"), []byte("value3"), 2, 3},
		}

		for _, e := range entries {
			err := store.LogInsert(e.key, e.value, e.term, e.index)
			So(err, ShouldBeNil)
		}

		Convey("When creating a new store with the same WAL", func() {
			store.Close()
			newStore, err := NewPersistentStore(tmpDir)
			So(err, ShouldBeNil)
			defer newStore.Close()

			Convey("Then it should load the last state correctly", func() {
				term, index := newStore.GetLastState()
				So(term, ShouldEqual, entries[len(entries)-1].term)
				So(index, ShouldEqual, entries[len(entries)-1].index)
			})
		})
	})
}

func TestCreateSnapshot(t *testing.T) {
	Convey("Given a persistent store with entries", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)
		defer store.Close()

		// Add enough entries to trigger snapshot
		for i := uint64(1); i <= 1001; i++ {
			err := store.LogInsert([]byte("key"), []byte("value"), 1, i)
			So(err, ShouldBeNil)
		}

		Convey("When waiting for snapshot creation", func() {
			// Wait a bit for async snapshot to complete
			time.Sleep(100 * time.Millisecond)

			Convey("Then a snapshot file should exist", func() {
				files, err := os.ReadDir(filepath.Join(tmpDir, "snapshot"))
				So(err, ShouldBeNil)
				So(len(files), ShouldBeGreaterThan, 0)

				Convey("And the WAL should be truncated", func() {
					walInfo, err := os.Stat(filepath.Join(tmpDir, "wal.log"))
					So(err, ShouldBeNil)
					// WAL should now only contain the snapshot entry
					So(walInfo.Size(), ShouldBeLessThan, 100)
				})
			})
		})
	})
}

func TestTruncateWAL(t *testing.T) {
	Convey("Given a persistent store with a large WAL", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)
		defer store.Close()

		// Add many entries
		for i := uint64(1); i <= 100; i++ {
			err := store.LogInsert([]byte("key"), []byte("value"), 1, i)
			So(err, ShouldBeNil)
		}

		store.drainBatch()

		originalSize, err := os.Stat(filepath.Join(tmpDir, "wal.log"))
		So(err, ShouldBeNil)

		Convey("When truncating the WAL", func() {
			err := store.truncateWAL()
			So(err, ShouldBeNil)

			Convey("Then the WAL should be smaller", func() {
				newSize, err := os.Stat(filepath.Join(tmpDir, "wal.log"))
				So(err, ShouldBeNil)
				So(newSize.Size(), ShouldBeLessThan, originalSize.Size())
			})

			Convey("And the state should be preserved", func() {
				term, index := store.GetLastState()
				So(term, ShouldEqual, uint64(1))
				So(index, ShouldEqual, uint64(100))
			})
		})
	})
}

func TestGetLastState(t *testing.T) {
	Convey("Given a persistent store", t, func() {
		tmpDir := filepath.Join(os.TempDir(), "radix-test-"+time.Now().Format("20060102150405"))
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)
		defer store.Close()

		Convey("When getting initial state", func() {
			term, index := store.GetLastState()
			So(term, ShouldEqual, uint64(0))
			So(index, ShouldEqual, uint64(0))
		})

		Convey("When getting state after updates", func() {
			err := store.LogInsert([]byte("key"), []byte("value"), 2, 5)
			So(err, ShouldBeNil)

			term, index := store.GetLastState()
			So(term, ShouldEqual, uint64(2))
			So(index, ShouldEqual, uint64(5))
		})
	})
}

/*
tempPersistDir returns a unique directory under os.TempDir for isolated WAL tests.
*/
func tempPersistDir() string {
	var suffix [4]byte

	_, _ = rand.Read(suffix[:])

	return filepath.Join(
		os.TempDir(),
		fmt.Sprintf("dmt-persist-%d-%x", time.Now().UnixNano(), suffix),
	)
}

/*
TestLogInsertOnClosedStore verifies LogInsert fails after Close.
*/
func TestLogInsertOnClosedStore(t *testing.T) {
	Convey("Given a closed persistent store", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)

		err = store.Close()
		So(err, ShouldBeNil)

		Convey("When logging an insert after close", func() {
			err := store.LogInsert([]byte("k"), []byte("v"), 1, 1)
			So(err, ShouldNotBeNil)
		})
	})
}

/*
TestLogTermOnClosedStore verifies LogTerm fails after Close.
*/
func TestLogTermOnClosedStore(t *testing.T) {
	Convey("Given a closed persistent store", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)

		err = store.Close()
		So(err, ShouldBeNil)

		Convey("When logging a term update after close", func() {
			err := store.LogTerm(1)
			So(err, ShouldNotBeNil)
		})
	})
}

/*
TestLogTerm verifies term-only WAL records advance lastTerm without advancing index.
*/
func TestLogTerm(t *testing.T) {
	Convey("Given a fresh persistent store", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)
		defer store.Close()

		Convey("When logging term 5", func() {
			err := store.LogTerm(5)
			So(err, ShouldBeNil)

			term, index := store.GetLastState()
			So(term, ShouldEqual, uint64(5))
			So(index, ShouldEqual, uint64(0))

			Convey("And logging term 10", func() {
				err := store.LogTerm(10)
				So(err, ShouldBeNil)

				term, index := store.GetLastState()
				So(term, ShouldEqual, uint64(10))
				So(index, ShouldEqual, uint64(0))
			})
		})
	})
}

/*
TestReplayMixedOperations verifies Replay returns only inserts while term updates still affect state.
*/
func TestReplayMixedOperations(t *testing.T) {
	Convey("Given a WAL with inserts and a term update", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)

		So(store.LogInsert([]byte("a1"), []byte("v1"), 1, 1), ShouldBeNil)
		So(store.LogInsert([]byte("a2"), []byte("v2"), 1, 2), ShouldBeNil)
		So(store.LogInsert([]byte("a3"), []byte("v3"), 1, 3), ShouldBeNil)
		So(store.LogTerm(5), ShouldBeNil)
		So(store.LogInsert([]byte("a4"), []byte("v4"), 5, 4), ShouldBeNil)
		So(store.LogInsert([]byte("a5"), []byte("v5"), 5, 5), ShouldBeNil)

		So(store.Close(), ShouldBeNil)

		Convey("When reopening and replaying", func() {
			newStore, err := NewPersistentStore(tmpDir)
			So(err, ShouldBeNil)
			defer newStore.Close()

			entries, replayErr := newStore.Replay()
			So(replayErr, ShouldBeNil)
			So(len(entries), ShouldEqual, 5)

			for _, entry := range entries {
				So(entry.Op, ShouldEqual, opInsert)
			}

			term, index := newStore.GetLastState()
			So(term, ShouldEqual, uint64(5))
			So(index, ShouldEqual, uint64(5))
		})
	})
}

/*
TestReplayOnEmptyDir verifies Replay on a brand-new WAL yields no entries and no error.
*/
func TestReplayOnEmptyDir(t *testing.T) {
	Convey("Given a new store on an empty directory", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)
		defer store.Close()

		Convey("When replaying immediately", func() {
			entries, replayErr := store.Replay()
			So(replayErr, ShouldBeNil)
			So(len(entries), ShouldEqual, 0)
			So(entries, ShouldBeNil)
		})
	})
}

/*
TestReplayPreservesOrder verifies insert entries return in WAL order after restart.
*/
func TestReplayPreservesOrder(t *testing.T) {
	Convey("Given sequential inserts", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)

		for i := 0; i < 5; i++ {
			key := []byte(fmt.Sprintf("order-%d", i))
			val := []byte(fmt.Sprintf("val-%d", i))
			So(store.LogInsert(key, val, 1, uint64(i+1)), ShouldBeNil)
		}

		So(store.Close(), ShouldBeNil)

		Convey("When reopening and replaying", func() {
			newStore, err := NewPersistentStore(tmpDir)
			So(err, ShouldBeNil)
			defer newStore.Close()

			entries, replayErr := newStore.Replay()
			So(replayErr, ShouldBeNil)
			So(len(entries), ShouldEqual, 5)

			for i := 0; i < 5; i++ {
				So(string(entries[i].Key), ShouldEqual, fmt.Sprintf("order-%d", i))
				So(string(entries[i].Value), ShouldEqual, fmt.Sprintf("val-%d", i))
			}
		})
	})
}

/*
TestDoubleClose verifies Close is idempotent.
*/
func TestDoubleClose(t *testing.T) {
	Convey("Given a persistent store", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)

		Convey("When closing twice", func() {
			So(store.Close(), ShouldBeNil)
			So(store.Close(), ShouldBeNil)
		})
	})
}

/*
TestConcurrentLogInsert stresses LogInsert from many goroutines with unique indices.

Indices are confined to [1001,1500] and [2001,2500] so no insert satisfies index%1000==0,
which would schedule createSnapshot and truncate the WAL while other goroutines append.
*/
func TestConcurrentLogInsert(t *testing.T) {
	Convey("Given a persistent store", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)

		var wg sync.WaitGroup

		var mu sync.Mutex
		var firstErr error

		for goroutineID := 0; goroutineID < 10; goroutineID++ {
			wg.Add(1)

			go func(gid int) {
				defer wg.Done()

				var base uint64

				if gid < 5 {
					base = 1001 + uint64(gid*100)
				} else {
					base = 2001 + uint64((gid-5)*100)
				}

				for i := uint64(1); i <= 100; i++ {
					key := []byte(fmt.Sprintf("c-%d-%d", gid, i))
					walIndex := base + i - 1
					insertErr := store.LogInsert(key, []byte("v"), 1, walIndex)

					if insertErr != nil {
						mu.Lock()

						if firstErr == nil {
							firstErr = insertErr
						}

						mu.Unlock()

						return
					}
				}
			}(goroutineID)
		}

		wg.Wait()
		So(firstErr, ShouldBeNil)

		So(store.Close(), ShouldBeNil)

		Convey("When reopening", func() {
			newStore, openErr := NewPersistentStore(tmpDir)
			So(openErr, ShouldBeNil)
			defer newStore.Close()

			entries, replayErr := newStore.Replay()
			So(replayErr, ShouldBeNil)
			So(len(entries), ShouldEqual, 1000)

			for _, entry := range entries {
				So(entry.Op, ShouldEqual, opInsert)
				So(entry.Term, ShouldEqual, uint64(1))
			}
		})
	})
}

/*
TestPersistLargeValues verifies multi-megabyte key and value round-trip through the WAL.
*/
func TestPersistLargeValues(t *testing.T) {
	Convey("Given a persistent store", t, func() {
		tmpDir := tempPersistDir()
		defer os.RemoveAll(tmpDir)

		store, err := NewPersistentStore(tmpDir)
		So(err, ShouldBeNil)

		const oneMB = 1 << 20

		key := make([]byte, oneMB)
		value := make([]byte, oneMB)

		for i := range key {
			key[i] = byte(i % 251)
		}

		for i := range value {
			value[i] = byte((i + 13) % 251)
		}

		So(store.LogInsert(key, value, 1, 1), ShouldBeNil)
		So(store.Close(), ShouldBeNil)

		Convey("When reopening and replaying", func() {
			newStore, err := NewPersistentStore(tmpDir)
			So(err, ShouldBeNil)
			defer newStore.Close()

			entries, replayErr := newStore.Replay()
			So(replayErr, ShouldBeNil)
			So(len(entries), ShouldEqual, 1)
			So(entries[0].Op, ShouldEqual, opInsert)
			So(bytes.Equal(entries[0].Key, key), ShouldBeTrue)
			So(bytes.Equal(entries[0].Value, value), ShouldBeTrue)
		})
	})
}

/*
BenchmarkLogInsert measures single insert throughput on a warm store.
*/
func BenchmarkLogInsert(b *testing.B) {
	tmpDir := tempPersistDir()
	defer os.RemoveAll(tmpDir)

	store, err := NewPersistentStore(tmpDir)

	if err != nil {
		b.Fatal(err)
	}

	defer store.Close()

	key := []byte("bench-key")
	val := []byte("bench-val")
	var index uint64

	b.ReportAllocs()

	for b.Loop() {
		index++

		if err := store.LogInsert(key, val, 1, index); err != nil {
			b.Fatal(err)
		}
	}
}

/*
BenchmarkReplay measures NewPersistentStore replay cost after 1000 inserts.
*/
func BenchmarkReplay(b *testing.B) {
	tmpDir := tempPersistDir()
	defer os.RemoveAll(tmpDir)

	store, err := NewPersistentStore(tmpDir)

	if err != nil {
		b.Fatal(err)
	}

	for i := uint64(1); i <= 1000; i++ {
		if err := store.LogInsert([]byte("k"), []byte("v"), 1, i); err != nil {
			store.Close()
			b.Fatal(err)
		}
	}

	store.Close()

	b.ReportAllocs()

	for b.Loop() {
		replayStore, openErr := NewPersistentStore(tmpDir)

		if openErr != nil {
			b.Fatal(openErr)
		}

		replayStore.Close()
	}
}

/*
BenchmarkLogTerm measures term-update WAL writes.
*/
func BenchmarkLogTerm(b *testing.B) {
	tmpDir := tempPersistDir()
	defer os.RemoveAll(tmpDir)

	store, err := NewPersistentStore(tmpDir)

	if err != nil {
		b.Fatal(err)
	}

	defer store.Close()

	var term uint64

	b.ReportAllocs()

	for b.Loop() {
		term++

		if err := store.LogTerm(term); err != nil {
			b.Fatal(err)
		}
	}
}

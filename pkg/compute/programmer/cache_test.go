package programmer

import (
	"fmt"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestProgramCache_LRUeviction(t *testing.T) {
	Convey("Given a tiny program cache", t, func() {
		SetProgramCacheMaxEntries(2)
		defer SetProgramCacheMaxEntries(ProgramCacheMaxEntries)

		Convey("It should evict the least-recently-used entry when over capacity", func() {
			setCachedProgram("k0", cachedProgram{})
			setCachedProgram("k1", cachedProgram{})
			setCachedProgram("k2", cachedProgram{})

			_, ok0 := getCachedProgram("k0")
			_, ok1 := getCachedProgram("k1")
			_, ok2 := getCachedProgram("k2")

			So(ok0, ShouldBeFalse)
			So(ok1, ShouldBeTrue)
			So(ok2, ShouldBeTrue)
		})
	})
}

func TestProgramCache_ConcurrentGetSet(t *testing.T) {
	const workers = 32
	const iters = 200

	SetProgramCacheMaxEntries(64)
	defer SetProgramCacheMaxEntries(ProgramCacheMaxEntries)

	var barrier sync.WaitGroup
	barrier.Add(workers)

	for worker := 0; worker < workers; worker++ {
		go func(base int) {
			defer barrier.Done()

			for iter := 0; iter < iters; iter++ {
				key := fmt.Sprintf("w%d-%d", base, iter%8)
				setCachedProgram(key, cachedProgram{})
				_, _ = getCachedProgram(key)
			}
		}(worker)
	}

	barrier.Wait()
}

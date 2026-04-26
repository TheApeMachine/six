package primitive

import (
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHeapFallbackAllocations(t *testing.T) {
	Convey("Given the arena has no free slots left", t, func() {
		ensureArena()

		savedNext := atomic.LoadUint32(&arenaLinearNext)
		savedFallbacks := heapFallbackAllocations.Load()

		arenaMutex.Lock()
		savedFree := append([]uint32(nil), freeArenaIdx...)
		freeArenaIdx = nil
		arenaMutex.Unlock()

		heapFallbackAllocations.Store(0)
		atomic.StoreUint32(&arenaLinearNext, uint32(len(valueArena)))

		value := AllocValue()

		Reset(func() {
			FreeValue(value)
			heapFallbackAllocations.Store(savedFallbacks)
			atomic.StoreUint32(&arenaLinearNext, savedNext)

			arenaMutex.Lock()
			freeArenaIdx = savedFree
			arenaMutex.Unlock()
		})

		Convey("AllocValue should report the heap fallback", func() {
			_, ok := ArenaIndex(value)

			So(ok, ShouldBeFalse)
			So(HeapFallbackAllocations(), ShouldEqual, 1)
		})
	})
}

func TestFreeValue(t *testing.T) {
	Convey("Given an arena Value is closed twice", t, func() {
		value := AllocValue()
		slot, ok := ArenaIndex(value)

		So(ok, ShouldBeTrue)

		So(value.Close(), ShouldBeNil)
		So(value.Close(), ShouldBeNil)

		Convey("It should publish the slot at most once", func() {
			seen := 0

			arenaMutex.Lock()
			for _, freeSlot := range freeArenaIdx {
				if freeSlot == slot {
					seen++
				}
			}
			arenaMutex.Unlock()

			So(seen, ShouldEqual, 1)
		})
	})
}

package compute

import (
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
)

func TestPrioritySpillRingTryPushTryPopFIFO(t *testing.T) {

	convey.Convey("Given a small prioritySpillRing", t, func() {
		ring := newPrioritySpillRing(8)

		convey.Convey("tryPop on empty returns nil", func() {
			convey.So(ring.tryPop() == nil, convey.ShouldBeTrue)
		})

		var a, b, c [128]uint64
		pa := unsafe.Pointer(&a)
		pb := unsafe.Pointer(&b)
		pc := unsafe.Pointer(&c)

		convey.Convey("FIFO order holds for sequential push then pop", func() {
			convey.So(ring.tryPush(pa), convey.ShouldBeTrue)
			convey.So(ring.tryPush(pb), convey.ShouldBeTrue)
			convey.So(ring.tryPush(pc), convey.ShouldBeTrue)
			convey.So(ring.tryPop(), convey.ShouldEqual, pa)
			convey.So(ring.tryPop(), convey.ShouldEqual, pb)
			convey.So(ring.tryPop(), convey.ShouldEqual, pc)
			convey.So(ring.tryPop() == nil, convey.ShouldBeTrue)
		})
	})
}

func TestPrioritySpillRingTryPushWhenFull(t *testing.T) {

	convey.Convey("Given a capacity-2 ring filled without popping", t, func() {
		ring := newPrioritySpillRing(2)
		var x, y, z [128]uint64

		convey.So(ring.tryPush(unsafe.Pointer(&x)), convey.ShouldBeTrue)
		convey.So(ring.tryPush(unsafe.Pointer(&y)), convey.ShouldBeTrue)
		convey.So(ring.tryPush(unsafe.Pointer(&z)), convey.ShouldBeFalse)

		convey.Convey("after one pop, another push succeeds", func() {
			convey.So(ring.tryPop(), convey.ShouldEqual, unsafe.Pointer(&x))
			convey.So(ring.tryPush(unsafe.Pointer(&z)), convey.ShouldBeTrue)
		})
	})
}

func TestPrioritySpillRingTryPushConcurrentWithTryPop(t *testing.T) {

	convey.Convey("MPMC stress: two producers and main-thread drain count all pushes", t, func() {
		const capacity = 256
		const perProducer = 4000

		ring := newPrioritySpillRing(capacity)
		var producers sync.WaitGroup
		producers.Add(2)

		runProducer := func(base int) {
			defer producers.Done()

			for offset := 0; offset < perProducer; offset++ {
				word := new([128]uint64)
				word[0] = uint64(base + offset)
				ptr := unsafe.Pointer(word)

				for !ring.tryPush(ptr) {
					runtime.Gosched()
				}
			}
		}

		go runProducer(0)
		go runProducer(perProducer)

		popped := 0
		target := perProducer * 2

		drainTimeout := time.NewTimer(30 * time.Second)
		defer drainTimeout.Stop()

		for popped < target {
			select {
			case <-drainTimeout.C:
				t.Fatalf(
					"TestPrioritySpillRingTryPushConcurrentWithTryPop: drain timed out (ring.tryPop stalled; popped=%d target=%d perProducer=%d runProducer)",
					popped,
					target,
					perProducer,
				)
			default:
				ptr := ring.tryPop()
				if ptr == nil {
					runtime.Gosched()

					continue
				}

				popped++
			}
		}

		producers.Wait()
		convey.So(popped, convey.ShouldEqual, target)
	})
}

func BenchmarkPrioritySpillRingTryPushTryPop(b *testing.B) {

	ring := newPrioritySpillRing(1024)
	var blob [128]uint64
	ptr := unsafe.Pointer(&blob)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		for !ring.tryPush(ptr) {
		}
		for ring.tryPop() == nil {
		}
	}
}

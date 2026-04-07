package data

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewRing(t *testing.T) {
	Convey("Given a valid context and power-of-two capacity", t, func() {
		ring, err := NewRing(context.Background(), 8)

		Convey("NewRing should return a usable ring and no validation error", func() {
			So(err, ShouldBeNil)
			So(ring, ShouldNotBeNil)
			So(ring.ctx, ShouldNotBeNil)
			So(ring.Error(), ShouldBeNil)
		})
	})
}

func TestRingPush(t *testing.T) {
	Convey("Given a nil *Ring", t, func() {
		var ring *Ring

		Convey("Push should refuse without panicking", func() {
			var blob [128]uint64

			So(ring.Push(unsafe.Pointer(&blob)), ShouldBeFalse)
		})
	})

	Convey("Given a small Ring", t, func() {
		ring, err := NewRing(context.Background(), 8)
		So(err, ShouldBeNil)

		var a, b, c [128]uint64

		pointerA := unsafe.Pointer(&a)
		pointerB := unsafe.Pointer(&b)
		pointerC := unsafe.Pointer(&c)

		Convey("Pop on empty returns nil before any Push", func() {
			So(ring.Pop() == nil, ShouldBeTrue)
		})

		Convey("FIFO order holds for sequential Push then Pop", func() {
			So(ring.Push(pointerA), ShouldBeTrue)
			So(ring.Push(pointerB), ShouldBeTrue)
			So(ring.Push(pointerC), ShouldBeTrue)
			So(ring.Pop(), ShouldEqual, pointerA)
			So(ring.Pop(), ShouldEqual, pointerB)
			So(ring.Pop(), ShouldEqual, pointerC)
			So(ring.Pop() == nil, ShouldBeTrue)
		})
	})

	Convey("Given a capacity-2 Ring filled without popping", t, func() {
		ring, err := NewRing(context.Background(), 2)
		So(err, ShouldBeNil)

		var x, y, z [128]uint64

		Convey("Push should fail when every slot holds a frame", func() {
			So(ring.Push(unsafe.Pointer(&x)), ShouldBeTrue)
			So(ring.Push(unsafe.Pointer(&y)), ShouldBeTrue)
			So(ring.Push(unsafe.Pointer(&z)), ShouldBeFalse)
		})

		Convey("after one Pop another Push succeeds", func() {
			So(ring.Push(unsafe.Pointer(&x)), ShouldBeTrue)
			So(ring.Push(unsafe.Pointer(&y)), ShouldBeTrue)
			So(ring.Push(unsafe.Pointer(&z)), ShouldBeFalse)
			So(ring.Pop(), ShouldEqual, unsafe.Pointer(&x))
			So(ring.Push(unsafe.Pointer(&z)), ShouldBeTrue)
		})
	})

	Convey("MPMC stress: two producers and main-thread drain count all pushes", t, func() {
		const capacity = 256
		const perProducer = 4000

		ring, err := NewRing(context.Background(), capacity)
		So(err, ShouldBeNil)

		var producers sync.WaitGroup

		producers.Add(2)

		runProducer := func(base int) {
			defer producers.Done()

			for offset := 0; offset < perProducer; offset++ {
				word := new([128]uint64)
				word[0] = uint64(base + offset)
				ptr := unsafe.Pointer(word)

				for !ring.Push(ptr) {
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
					"TestRingPush: drain timed out (Pop stalled; popped=%d target=%d perProducer=%d)",
					popped,
					target,
					perProducer,
				)
			default:
				ptr := ring.Pop()
				if ptr == nil {
					runtime.Gosched()

					continue
				}

				popped++
			}
		}

		producers.Wait()
		So(popped, ShouldEqual, target)
	})
}

func TestRingPop(t *testing.T) {
	Convey("Given a nil *Ring", t, func() {
		var ring *Ring

		Convey("Pop should return nil without panicking", func() {
			So(ring.Pop() == nil, ShouldBeTrue)
		})
	})
}

func TestRingClose(t *testing.T) {
	Convey("Given a Ring from NewRing", t, func() {
		ring, err := NewRing(context.Background(), 4)
		So(err, ShouldBeNil)

		Convey("Close should cancel the derived context", func() {
			closeErr := ring.Close()

			So(closeErr, ShouldBeNil)
			So(ring.ctx.Err(), ShouldNotBeNil)
		})
	})

	Convey("Given a nil *Ring", t, func() {
		var ring *Ring

		Convey("Close should be a no-op", func() {
			So(ring.Close(), ShouldBeNil)
		})
	})
}

func TestRingError(t *testing.T) {
	Convey("Given a Ring from NewRing", t, func() {
		ring, err := NewRing(context.Background(), 4)
		So(err, ShouldBeNil)

		Convey("Error should return the stored failure until one is set", func() {
			So(ring.Error(), ShouldBeNil)
		})
	})
}

func BenchmarkRingPush(b *testing.B) {
	ring, err := NewRing(context.Background(), 1024)

	if err != nil {
		b.Fatal(err)
	}

	var blob [128]uint64
	ptr := unsafe.Pointer(&blob)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		for !ring.Push(ptr) {
		}

		for ring.Pop() == nil {
		}
	}
}

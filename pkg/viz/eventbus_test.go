package viz

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewBus(t *testing.T) {
	t.Parallel()

	Convey("NewBus starts inactive", t, func() {
		bus := NewBus()

		So(bus.IsActive(), ShouldBeFalse)
	})
}

func TestBusActivate(t *testing.T) {
	t.Parallel()

	Convey("Activate enables publishing", t, func() {
		bus := NewBus()

		bus.Activate()

		So(bus.IsActive(), ShouldBeTrue)
	})
}

func TestBusDeactivate(t *testing.T) {
	t.Parallel()

	Convey("Deactivate silences Publish", t, func() {
		bus := NewBus()

		bus.Activate()

		ch := bus.Subscribe(4, nil)

		bus.Deactivate()
		bus.Publish(NewEvent(EventNodeCreated, "x"))

		select {
		case <-ch:
			t.Fatal("unexpected delivery while inactive")
		case <-time.After(20 * time.Millisecond):
		}

		bus.Unsubscribe(ch)
	})
}

func TestBusIsActive(t *testing.T) {
	t.Parallel()

	Convey("IsActive tracks Activate/Deactivate", t, func() {
		bus := NewBus()

		So(bus.IsActive(), ShouldBeFalse)

		bus.Activate()

		So(bus.IsActive(), ShouldBeTrue)

		bus.Deactivate()

		So(bus.IsActive(), ShouldBeFalse)
	})
}

func TestBusDropped(t *testing.T) {
	t.Parallel()

	Convey("Publish increments Dropped when the buffer is full", t, func() {
		bus := NewBus()

		bus.Activate()

		ch := bus.Subscribe(1, nil)

		bus.Publish(NewEvent(EventGossipSent, "a"))
		bus.Publish(NewEvent(EventGossipSent, "b"))

		So(bus.Dropped(), ShouldEqual, 1)

		<-ch

		bus.Unsubscribe(ch)
	})
}

func TestBusSubscribe(t *testing.T) {
	t.Parallel()

	Convey("Subscribe delivers matching events", t, func() {
		bus := NewBus()

		bus.Activate()

		ch := bus.Subscribe(8, func(ev Event) bool {
			return ev.Kind == EventTrieInsert
		})

		bus.Publish(NewEvent(EventTrieInsert, "src"))

		ev := <-ch

		So(ev.Kind, ShouldEqual, EventTrieInsert)

		bus.Unsubscribe(ch)
	})
}

func TestBusUnsubscribe(t *testing.T) {
	t.Parallel()

	Convey("Unsubscribe stops delivery and closes the channel", t, func() {
		bus := NewBus()

		bus.Activate()

		ch := bus.Subscribe(4, nil)

		bus.Unsubscribe(ch)

		_, open := <-ch

		So(open, ShouldBeFalse)
	})
}

func TestBusPublish(t *testing.T) {
	t.Parallel()

	Convey("Publish fans out to all subscribers", t, func() {
		bus := NewBus()

		bus.Activate()

		first := bus.Subscribe(8, nil)
		second := bus.Subscribe(8, nil)

		ev := NewEvent(EventBeamCollect, "node")

		bus.Publish(ev)

		So(<-first, ShouldResemble, ev)
		So(<-second, ShouldResemble, ev)

		bus.Unsubscribe(first)
		bus.Unsubscribe(second)
	})
}

func TestNewEvent(t *testing.T) {
	t.Parallel()

	Convey("NewEvent stamps kind, time, and initializes maps", t, func() {
		ev := NewEvent(EventAdaptiveUpdate, "core")

		So(ev.Kind, ShouldEqual, EventAdaptiveUpdate)
		So(ev.Source, ShouldEqual, "core")
		So(ev.Timestamp, ShouldBeGreaterThan, int64(0))
		So(ev.Values, ShouldNotBeNil)
		So(ev.Meta, ShouldNotBeNil)
	})
}

func BenchmarkBusPublish(b *testing.B) {
	bus := NewBus()

	bus.Activate()

	sub := bus.Subscribe(4096, nil)

	b.Cleanup(func() {
		bus.Unsubscribe(sub)
	})

	ev := NewEvent(EventPoolSchedule, "bench")

	b.ResetTimer()

	for b.Loop() {
		bus.Publish(ev)
		for len(sub) > 0 {
			<-sub
		}
	}
}

package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBroadcastGroupUnsubscribe(t *testing.T) {
	Convey("Given a subscriber", t, func() {
		bg := NewBroadcastGroup("g", time.Minute, 10)
		ch := bg.Subscribe("sub-1", 5)

		Convey("Unsubscribe should close the channel", func() {
			bg.Unsubscribe("sub-1")
			_, ok := <-ch
			So(ok, ShouldBeFalse)
		})
	})
}

func TestBroadcastGroupAddFilterDropsSend(t *testing.T) {
	Convey("Given a global filter that rejects results", t, func() {
		bg := NewBroadcastGroup("g", time.Minute, 10)
		ch := bg.Subscribe("sub-1", 5)
		bg.AddFilter(func(r *Result) bool {
			return r.Value != nil
		})

		Convey("Send of nil-valued result should drop and not deliver", func() {
			bg.Send(NewResult(nil))
			select {
			case <-ch:
				t.Fatal("unexpected delivery")
			case <-time.After(50 * time.Millisecond):
			}
			m := bg.GetMetrics()
			So(m.MessagesDropped, ShouldBeGreaterThan, 0)
		})
	})
}

func TestBroadcastGroupRoutingRules(t *testing.T) {
	Convey("Given per-subscriber routing rules", t, func() {
		bg := NewBroadcastGroup("g", time.Minute, 10)
		yesCh := bg.Subscribe("yes", 5, RoutingRule{
			SubscriberID: "yes",
			Filter: func(r *Result) bool {
				v, ok := r.Value.(int)
				return ok && v == 1
			},
		})
		noCh := bg.Subscribe("no", 5, RoutingRule{
			SubscriberID: "no",
			Filter: func(r *Result) bool {
				v, ok := r.Value.(int)
				return ok && v == 2
			},
		})

		Convey("Send should reach only matching subscribers", func() {
			bg.Send(NewResult(1))
			select {
			case got := <-yesCh:
				So(got.Value, ShouldEqual, 1)
			case <-time.After(time.Second):
				t.Fatal("yes subscriber timed out")
			}
			select {
			case <-noCh:
				t.Fatal("unexpected delivery to filtered subscriber")
			case <-time.After(50 * time.Millisecond):
			}
		})
	})
}

func TestBroadcastGroupSendAfterClose(t *testing.T) {
	Convey("Given a closed broadcast group", t, func() {
		bg := NewBroadcastGroup("g", time.Minute, 10)
		ch := bg.Subscribe("s", 2)
		bg.Close()

		Convey("Send should be a no-op", func() {
			bg.Send(NewResult("late"))
			select {
			case _, ok := <-ch:
				if ok {
					t.Fatal("unexpected delivery after close")
				}
			case <-time.After(50 * time.Millisecond):
			}
		})
	})
}

func TestBroadcastGroupGetMetrics(t *testing.T) {
	Convey("Given a broadcast group with traffic", t, func() {
		bg := NewBroadcastGroup("g", time.Minute, 10)
		_ = bg.Subscribe("s", 10)
		bg.Send(NewResult("ping"))

		Convey("GetMetrics should reflect counts", func() {
			m := bg.GetMetrics()
			So(m.MessagesSent, ShouldBeGreaterThan, 0)
			So(m.BroadcastCount, ShouldEqual, 1)
			So(m.ActiveSubscribers, ShouldEqual, 1)
		})
	})
}

func TestBroadcastGroupAddRoutingRule(t *testing.T) {
	Convey("Given AddRoutingRule after Subscribe", t, func() {
		bg := NewBroadcastGroup("g", time.Minute, 10)
		ch := bg.Subscribe("late-rules", 5)
		bg.AddRoutingRule("late-rules", RoutingRule{
			SubscriberID: "late-rules",
			Filter: func(r *Result) bool {
				_, ok := r.Value.(string)
				return ok
			},
		})

		Convey("Send should honor late-added rules", func() {
			bg.Send(NewResult("ok"))
			select {
			case got := <-ch:
				So(got.Value, ShouldEqual, "ok")
			case <-time.After(time.Second):
				t.Fatal("timeout")
			}
		})
	})
}

func BenchmarkBroadcastGroupSend(b *testing.B) {
	bg := NewBroadcastGroup("bench", time.Minute, 1000)
	_ = bg.Subscribe("sub", 1000)
	r := NewResult([]byte("x"))
	b.ReportAllocs()
	for b.Loop() {
		bg.Send(r)
	}
}

func BenchmarkBroadcastGroupUnsubscribe(b *testing.B) {
	bg := NewBroadcastGroup("bench-unsub", time.Minute, 64)
	var seq int64
	b.ReportAllocs()
	for b.Loop() {
		seq++
		id := "sub-" + time.Duration(seq).String()
		_ = bg.Subscribe(id, 1)
		bg.Unsubscribe(id)
	}
}

func BenchmarkBroadcastGroupFiltersAndRouting(b *testing.B) {
	bg := NewBroadcastGroup("bench-rules", time.Minute, 128)
	_ = bg.Subscribe("sub", 128)
	bg.AddFilter(func(r *Result) bool { return r != nil })
	bg.AddRoutingRule("sub", RoutingRule{
		SubscriberID: "sub",
		Filter: func(r *Result) bool {
			return r != nil
		},
	})

	r := NewResult("ok")
	b.ReportAllocs()
	for b.Loop() {
		bg.Send(r)
		_ = bg.GetMetrics()
	}
}

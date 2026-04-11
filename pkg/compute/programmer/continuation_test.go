package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestContinuation_ApplyScheduling(t *testing.T) {
	original := *core.Cfg
	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given a nil continuation", t, func() {
		var cont *Continuation
		var value primitive.Value

		Convey("ApplyScheduling should not write", func() {
			cont.ApplyScheduling(&value)

			So(value[kernel.SchedulingNextProgramWord], ShouldEqual, uint64(0))
		})
	})

	Convey("Given ContinuationNone", t, func() {
		cont := &Continuation{Kind: ContinuationNone}
		var value primitive.Value

		Convey("ApplyScheduling should not write", func() {
			cont.ApplyScheduling(&value)

			So(value[kernel.SchedulingNextProgramWord], ShouldEqual, uint64(0))
		})
	})

	Convey("Given ContinuationValueID", t, func() {
		cont := &Continuation{Kind: ContinuationValueID, ValueID: 4242}
		var value primitive.Value

		Convey("ApplyScheduling should stamp word 117 with the value id", func() {
			cont.ApplyScheduling(&value)

			So(value[kernel.SchedulingNextProgramWord], ShouldEqual, uint64(4242))
		})
	})

	Convey("Given ContinuationSelf", t, func() {
		idWord := core.Cfg.Value.Region.ID.Start
		cont := &Continuation{Kind: ContinuationSelf}
		var value primitive.Value
		value.Set(idWord, 9001)

		Convey("ApplyScheduling should stamp word 117 with the frame id", func() {
			cont.ApplyScheduling(&value)

			So(value[kernel.SchedulingNextProgramWord], ShouldEqual, uint64(9001))
		})
	})

	Convey("Given a nil Value", t, func() {
		cont := &Continuation{Kind: ContinuationValueID, ValueID: 1}

		Convey("ApplyScheduling should not panic", func() {
			cont.ApplyScheduling(nil)
		})
	})
}

func BenchmarkContinuation_ApplyScheduling(b *testing.B) {
	original := *core.Cfg
	b.Cleanup(func() {
		*core.Cfg = original
	})

	cont := &Continuation{Kind: ContinuationValueID, ValueID: 3}
	var value primitive.Value

	b.ResetTimer()

	for range b.N {
		cont.ApplyScheduling(&value)
	}
}

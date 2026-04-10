package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestCompilerFinalize(t *testing.T) {
	t.Parallel()

	Convey("Finalize returns no emitted Values when no finalizer is installed", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("finalize-empty")))

		So(err, ShouldBeNil)

		defer raw.Close()

		emitted, finalizeErr := New(raw).Finalize()

		So(finalizeErr, ShouldBeNil)
		So(emitted, ShouldBeNil)
	})

	Convey("Finalize composes finalizers through the next callback", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("finalize-chain")))

		So(err, ShouldBeNil)

		defer raw.Close()

		order := make([]string, 0, 3)

		compiler := New(
			raw,
			CompilerWithFinalizer(Finalizer(func(
				value *primitive.Value,
				next FinalizeNext,
			) ([]*primitive.Value, error) {
				order = append(order, "outer:before")

				emitted, err := next(value)
				if err != nil {
					return nil, err
				}

				order = append(order, "outer:after")

				return emitted, nil
			})),
			CompilerWithFinalizer(Finalizer(func(
				value *primitive.Value,
				_ FinalizeNext,
			) ([]*primitive.Value, error) {
				order = append(order, "inner")

				return []*primitive.Value{value}, nil
			})),
		)

		emitted, finalizeErr := compiler.Finalize()

		So(finalizeErr, ShouldBeNil)
		So(emitted, ShouldResemble, []*primitive.Value{raw})
		So(order, ShouldResemble, []string{
			"outer:before",
			"inner",
			"outer:after",
		})
	})

	Convey("Finalize can inspect the Signals region without rerunning execution", t, func() {
		raw, err := primitive.FirstSegment(primitive.NewValue([]byte("finalize-signals")))

		So(err, ShouldBeNil)

		defer raw.Close()

		raw.Set(core.Cfg.Value.Region.Signals.Start, 0xFFFFFFFFFFFFFFFF)

		compiler := New(
			raw,
			CompilerWithFinalizer(Finalizer(func(
				value *primitive.Value,
				_ FinalizeNext,
			) ([]*primitive.Value, error) {
				signals := primitive.ScanSignalRegion(value)

				So(len(signals), ShouldBeGreaterThan, 0)
				So(signals[0].Kind, ShouldEqual, primitive.SignalOneRun)

				return []*primitive.Value{value}, nil
			})),
		)

		emitted, finalizeErr := compiler.Finalize()

		So(finalizeErr, ShouldBeNil)
		So(emitted, ShouldResemble, []*primitive.Value{raw})
	})
}

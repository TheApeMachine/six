package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestCompilerFinalize(t *testing.T) {
	t.Parallel()

	Convey("Given a raw value with no finalizer", t, func() {
		Convey("It should return no emitted Values when no finalizer is installed", func() {
			raw, err := primitive.FirstSegment(primitive.NewValue([]byte("finalize-empty")))

			So(err, ShouldBeNil)

			defer raw.Close()

			emitted, finalizeErr := New(raw).Finalize()

			So(finalizeErr, ShouldBeNil)
			So(emitted, ShouldBeNil)
		})
	})

	Convey("Given a compiler with chained finalizers", t, func() {
		Convey("It should compose finalizers through the next callback", func() {
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

					emitted, nextErr := next(value)
					if nextErr != nil {
						return nil, nextErr
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
	})

	Convey("Given a raw value with signals stamped in the frame", t, func() {
		Convey("It should let a finalizer inspect the Signals region without rerunning execution", func() {
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
	})
}

func BenchmarkCompilerFinalize(b *testing.B) {
	raw, err := primitive.FirstSegment(primitive.NewValue([]byte("bench-finalize")))

	if err != nil {
		b.Fatal(err)
	}

	defer raw.Close()

	compiler := New(
		raw,
		CompilerWithFinalizer(Finalizer(func(
			value *primitive.Value,
			next FinalizeNext,
		) ([]*primitive.Value, error) {
			return next(value)
		})),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, finErr := compiler.Finalize()

		if finErr != nil {
			b.Fatal(finErr)
		}
	}
}

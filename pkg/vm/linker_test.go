package vm

import (
	"bytes"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestNewLinker verifies the constructor initializes the Linker correctly.
*/
func TestNewLinker(t *testing.T) {
	Convey("Given a request for a new Linker", t, func() {
		linker := NewLinker()

		Convey("It should return a non-nil Linker instance", func() {
			So(linker, ShouldNotBeNil)
			So(linker.values, ShouldNotBeNil)
			So(len(linker.values), ShouldEqual, 0)
			So(linker.lastID, ShouldEqual, 0)
		})
	})
}

/*
TestLinker_Push verifies that Values are correctly added to the sliding window.
*/
func TestLinker_Push(t *testing.T) {
	Convey("Given a Linker instance", t, func() {
		linker := NewLinker()

		Convey("When pushing nil values", func() {
			linker.Push(nil, nil)

			Convey("It should ignore them", func() {
				So(len(linker.values), ShouldEqual, 0)
			})
		})

		Convey("When pushing valid Values", func() {
			values, err := primitive.NewValue([]byte("test"))
			So(err, ShouldBeNil)
			So(len(values), ShouldBeGreaterThan, 0)

			linker.Push(values...)

			Convey("It should append them to the internal slice", func() {
				So(len(linker.values), ShouldEqual, len(values))
				So(linker.values[0], ShouldEqual, values[0])
			})

			Reset(func() {
				primitive.CloseAll(values)
			})
		})
	})
}

/*
TestLinker_Pop verifies that the sliding window correctly yields Values and their linking assets.
*/
func TestLinker_Pop(t *testing.T) {
	Convey("Given a Linker instance", t, func() {
		linker := NewLinker()

		Convey("When popping from an empty window", func() {
			val, assets := linker.Pop()

			Convey("It should return nil", func() {
				So(val, ShouldBeNil)
				So(assets, ShouldBeNil)
			})
		})

		Convey("When popping from a window with only one Value", func() {
			values, err := primitive.NewValue([]byte("single"))
			So(err, ShouldBeNil)

			linker.Push(values[0])
			val, assets := linker.Pop()

			Convey("It should return nil since it needs a next Value to form a link", func() {
				So(val, ShouldBeNil)
				So(assets, ShouldBeNil)
			})

			Reset(func() {
				primitive.CloseAll(values)
			})
		})

		Convey("When popping from a window with multiple Values", func() {
			// Create a payload that will generate at least two segments
			capacity := int((core.Cfg.Value.Region.Tokens.Bits + 7) / 8 / 2)
			payload := bytes.Repeat([]byte{'A'}, capacity*3)
			values, err := primitive.NewValue(payload)
			So(err, ShouldBeNil)
			So(len(values), ShouldBeGreaterThanOrEqualTo, 2)

			linker.Push(values...)

			firstVal, assets := linker.Pop()

			Convey("It should return the first Value and its linking assets", func() {
				So(firstVal, ShouldNotBeNil)
				So(firstVal, ShouldEqual, values[0])
				So(assets, ShouldNotBeNil)
				So(len(assets), ShouldEqual, 1)

				// Check the asset region
				assetValue := assets[0].Value

				// Since it's the first value, lastID is 0, so the first word in the asset region should be 0
				So(assetValue.Get(primitive.AssetRegion)[0], ShouldEqual, 0)
				// The next value's ID should be at the second word in the asset region
				So(assetValue.Get(primitive.AssetRegion)[1], ShouldEqual, values[1].ID())
			})

			Convey("It should update the lastID and advance the window", func() {
				So(linker.lastID, ShouldEqual, values[0].ID())
				So(len(linker.values), ShouldEqual, len(values)-1)
				So(linker.values[0], ShouldEqual, values[1])
			})

			Reset(func() {
				primitive.CloseAll(values)
			})
		})
	})
}

func BenchmarkLinker_Push(b *testing.B) {
	values, err := primitive.NewValue([]byte("benchmark push"))
	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}
	defer primitive.CloseAll(values)

	linker := NewLinker()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		linker.Push(values[0])
	}
}

func BenchmarkLinker_Pop(b *testing.B) {
	values, err := primitive.NewValue([]byte("benchmark pop"))
	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}
	defer primitive.CloseAll(values)

	linker := NewLinker()

	// Pre-fill the linker to avoid allocation during the benchmark loop
	for i := 0; i < b.N+1; i++ {
		linker.values = append(linker.values, values[0])
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = linker.Pop()
	}
}

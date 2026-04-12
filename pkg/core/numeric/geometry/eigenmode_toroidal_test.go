package geometry

import (
	"context"
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSymbolFromPropertyBand(t *testing.T) {
	Convey("Given SymbolFromPropertyBand", t, func() {
		Convey("It should return 0 for empty slice", func() {
			So(SymbolFromPropertyBand(nil), ShouldEqual, 0)
			So(SymbolFromPropertyBand([]uint64{}), ShouldEqual, 0)
		})

		Convey("It should produce a stable symbol in 0..511 for an eight-word fold", func() {
			words := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
			s := SymbolFromPropertyBand(words)
			So(s, ShouldBeGreaterThanOrEqualTo, 0)
			So(s, ShouldBeLessThan, 512)
			So(SymbolFromPropertyBand(words), ShouldEqual, s)
		})

		Convey("It should ignore extra words beyond eight", func() {
			long := []uint64{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFFFF}
			short := []uint64{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
			So(SymbolFromPropertyBand(long), ShouldEqual, SymbolFromPropertyBand(short))
		})
	})
}

func TestEigenModeToroidal_BuildCooccurrence(t *testing.T) {
	Convey("Given EigenModeToroidal.BuildCooccurrence", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		emt, err := NewEigenModeToroidal(
			EigenWithContext(ctx),
			func(e *EigenModeToroidal) {
				e.cancel = cancel
				e.affinity = []uint64{1}
			},
		)

		So(err, ShouldBeNil)

		Convey("It should fill phase and frequency from a byte corpus", func() {
			emt.BuildCooccurrence([]byte("ababab"), 2)
			So(len(emt.frequency), ShouldEqual, 512)
			So(emt.phase[0], ShouldBeBetweenOrEqual, -math.Pi, math.Pi)
		})
	})
}

func BenchmarkSymbolFromPropertyBand(b *testing.B) {
	words := []uint64{1, 2, 3, 4, 5, 6, 7, 8}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = SymbolFromPropertyBand(words)
	}
}

func BenchmarkEigenModeToroidal_BuildCooccurrence(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emt, err := NewEigenModeToroidal(
		EigenWithContext(ctx),
		func(e *EigenModeToroidal) {
			e.cancel = cancel
			e.affinity = []uint64{1}
		},
	)

	if err != nil {
		b.Fatal(err)
	}

	corpus := []byte("ababab")

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		emt.BuildCooccurrence(corpus, 2)
	}
}

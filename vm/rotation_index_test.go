package vm

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/tokenizer"
)

func TestRotationIndexIntegration(t *testing.T) {
	Convey("Given a loader that ingests two known samples", t, func() {
		dataset := &loaderMockDataset{samples: []string{
			"Hello World",
			"Goodbye Moon",
		}}

		loader := NewLoader(
			LoaderWithTokenizer(
				tokenizer.NewUniversal(
					tokenizer.TokenizerWithDataset(dataset),
				),
			),
		)

		err := loader.Start()
		So(err, ShouldBeNil)

		rotIndex := loader.RotationIndex()
		So(rotIndex, ShouldNotBeNil)
		So(rotIndex.Size(), ShouldBeGreaterThan, 0)

		Convey("It should recall the exact continuation from 'Hello ' prefix", func() {
			rot := geometry.IdentityRotation()

			for _, b := range []byte("Hello ") {
				rot = rot.Compose(geometry.RotationForChord(data.BaseChord(b)))
			}

			continuation := rotIndex.BestContinuation(rot)
			So(continuation, ShouldNotBeNil)
			So(len(continuation), ShouldBeGreaterThan, 0)

			decoded := make([]byte, 0, len(continuation))

			for _, chord := range continuation {
				decoded = append(decoded, chord.BestByte())
			}

			So(string(decoded), ShouldEqual, "World")
		})

		Convey("It should recall the exact continuation from 'Goodbye ' prefix", func() {
			rot := geometry.IdentityRotation()

			for _, b := range []byte("Goodbye ") {
				rot = rot.Compose(geometry.RotationForChord(data.BaseChord(b)))
			}

			continuation := rotIndex.BestContinuation(rot)
			So(continuation, ShouldNotBeNil)
			So(len(continuation), ShouldBeGreaterThan, 0)

			decoded := make([]byte, 0, len(continuation))

			for _, chord := range continuation {
				decoded = append(decoded, chord.BestByte())
			}

			So(string(decoded), ShouldEqual, "Moon")
		})

		Convey("It should return nil for a prefix never seen", func() {
			rot := geometry.IdentityRotation()

			for _, b := range []byte("ZZZZZ") {
				rot = rot.Compose(geometry.RotationForChord(data.BaseChord(b)))
			}

			continuation := rotIndex.BestContinuation(rot)
			So(continuation, ShouldBeNil)
		})
	})
}

func BenchmarkRotationIndexRecall(b *testing.B) {
	dataset := &loaderMockDataset{samples: []string{
		"Hello World",
		"Goodbye Moon",
	}}

	loader := NewLoader(
		LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(dataset),
			),
		),
	)

	_ = loader.Start()
	rotIndex := loader.RotationIndex()

	rot := geometry.IdentityRotation()

	for _, b := range []byte("Hello ") {
		rot = rot.Compose(geometry.RotationForChord(data.BaseChord(b)))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rotIndex.BestContinuation(rot)
	}
}

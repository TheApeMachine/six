package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestBestOutputCandidate(t *testing.T) {
	Convey("Given a graph with source and sink", t, func() {
		g := New(Config{InitialNodes: 4})

		Convey("When sink has no active faces", func() {
			face := g.sink.BestFace()
			Convey("It should return delimiter 256", func() {
				So(face, ShouldEqual, 256)
			})
		})
		Convey("When sink has one active face", func() {
			chord := data.BaseChord(42)
			g.sink.Cube[42] = chord

			face := g.sink.BestFace()
			Convey("It should return the logical face", func() {
				So(face, ShouldEqual, 42)
			})
		})
		Convey("When multiple faces have data, it should return densest", func() {
			sparse := data.BaseChord(11)
			dense := data.BaseChord(10)
			g.sink.Cube[10] = dense
			g.sink.Cube[11] = sparse
			// Make face 10 denser via ChordOR
			extra := data.BaseChord(101)
			g.sink.Cube[10] = data.ChordOR(&g.sink.Cube[10], &extra)

			face := g.sink.BestFace()
			Convey("It should select the densest face", func() {
				So(face, ShouldEqual, 10)
			})
		})
	})
}

func TestSequencerDecay(t *testing.T) {
	Convey("Given sequencerDecay", t, func() {
		Convey("When phi is 0, it should return 1.0 (no decay)", func() {
			So(sequencerDecay(0), ShouldEqual, 1.0)
		})
		Convey("When phi is negative, it should return 1.0", func() {
			So(sequencerDecay(-0.5), ShouldEqual, 1.0)
		})
		Convey("When phi is in valid range, it should return phi", func() {
			So(sequencerDecay(0.5), ShouldEqual, 0.5)
		})
	})
}

func BenchmarkBestOutputCandidate(b *testing.B) {
	g := New(Config{InitialNodes: 4})
	for i := 0; i < 20; i++ {
		g.sink.Cube[i] = data.BaseChord(byte(i))
	}
	g.source.Cube[5] = data.BaseChord(10)

	b.ResetTimer()
	for range b.N {
		_ = g.sink.BestFace()
	}
}

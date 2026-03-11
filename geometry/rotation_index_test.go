package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestRotationIndex_PrefixRecall(t *testing.T) {
	Convey("Given a rotation index with one sample", t, func() {
		idx := NewRotationIndex()
		sample := []data.Chord{
			data.BaseChord('H'),
			data.BaseChord('e'),
			data.BaseChord('l'),
			data.BaseChord('l'),
			data.BaseChord('o'),
		}

		rot := IdentityRotation()
		for pos, chord := range sample {
			rot = rot.Compose(RotationForChord(chord))
			continuation := append([]data.Chord(nil), sample[pos+1:]...)
			idx.Insert(rot, RotationEntry{
				SampleID:     0,
				Position:     pos,
				Chord:        chord,
				Continuation: continuation,
			})
		}

		So(idx.Size(), ShouldBeGreaterThan, 0)

		Convey("It should recall the continuation from an exact prefix", func() {
			queryRot := IdentityRotation()
			queryRot = queryRot.Compose(RotationForChord(data.BaseChord('H')))
			queryRot = queryRot.Compose(RotationForChord(data.BaseChord('e')))
			queryRot = queryRot.Compose(RotationForChord(data.BaseChord('l')))

			continuation := idx.BestContinuation(queryRot)
			So(len(continuation), ShouldEqual, 2)
			So(continuation[0], ShouldResemble, data.BaseChord('l'))
			So(continuation[1], ShouldResemble, data.BaseChord('o'))
		})

		Convey("It should return nil for an unknown prefix", func() {
			queryRot := IdentityRotation()
			queryRot = queryRot.Compose(RotationForChord(data.BaseChord('X')))
			queryRot = queryRot.Compose(RotationForChord(data.BaseChord('Y')))

			continuation := idx.BestContinuation(queryRot)
			So(continuation, ShouldBeNil)
		})
	})
}

func BenchmarkRotationIndex_Insert(b *testing.B) {
	idx := NewRotationIndex()
	chord := data.BaseChord('A')
	rot := IdentityRotation()
	cont := []data.Chord{data.BaseChord('B'), data.BaseChord('C')}

	for i := 0; i < b.N; i++ {
		rot = rot.Compose(RotationForChord(chord))
		idx.Insert(rot, RotationEntry{
			SampleID:     0,
			Position:     i,
			Chord:        chord,
			Continuation: cont,
		})
	}
}

func BenchmarkRotationIndex_Lookup(b *testing.B) {
	idx := NewRotationIndex()
	chord := data.BaseChord('A')
	rot := IdentityRotation()

	for i := 0; i < 10000; i++ {
		rot = rot.Compose(RotationForChord(chord))
		idx.Insert(rot, RotationEntry{
			SampleID:     0,
			Position:     i,
			Chord:        chord,
			Continuation: []data.Chord{data.BaseChord('B')},
		})
	}

	queryRot := IdentityRotation()
	for i := 0; i < 500; i++ {
		queryRot = queryRot.Compose(RotationForChord(chord))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx.BestContinuation(queryRot)
	}
}

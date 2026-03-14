package vm

import (
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
staticChordSource is a test implementation of ChordSource for contract tests.
*/
type staticChordSource struct {
	samples [][]data.Chord
	idx     int
	err     error
}

func (s *staticChordSource) Next() bool {
	if s.idx >= len(s.samples) {
		return false
	}
	s.idx++
	return true
}

func (s *staticChordSource) Chords() []data.Chord {
	if s.idx == 0 || s.idx > len(s.samples) {
		return nil
	}
	return s.samples[s.idx-1]
}

func (s *staticChordSource) Error() error {
	return s.err
}

/*
runChordSourceContract exercises any ChordSource implementation against expected
chords and optional expected error. Reused across implementations.
*/
func runChordSourceContract(t *testing.T, src ChordSource, expectedChords [][]data.Chord, expectedErr error) {
	t.Helper()

	for sampleIdx := 0; sampleIdx < len(expectedChords); sampleIdx++ {
		ok := src.Next()
		So(ok, ShouldBeTrue)

		chords := src.Chords()
		So(len(chords), ShouldEqual, len(expectedChords[sampleIdx]))
		for i := range chords {
			So(chords[i].ActiveCount(), ShouldEqual, expectedChords[sampleIdx][i].ActiveCount())
		}
	}

	So(src.Next(), ShouldBeFalse)

	if expectedErr != nil {
		So(src.Error(), ShouldNotBeNil)
		So(src.Error().Error(), ShouldContainSubstring, expectedErr.Error())
	} else {
		So(src.Error(), ShouldBeNil)
	}
}

func TestChordSourceContract(t *testing.T) {
	Convey("Given a staticChordSource with normal sequences", t, func() {
		expected := [][]data.Chord{
			{data.BaseChord('a'), data.BaseChord('b')},
			{data.BaseChord('x')},
		}
		src := &staticChordSource{samples: expected}
		runChordSourceContract(t, src, expected, nil)
	})

	Convey("Given a staticChordSource that is empty", t, func() {
		src := &staticChordSource{samples: [][]data.Chord{}}
		So(src.Next(), ShouldBeFalse)
		So(src.Chords(), ShouldBeNil)
		So(src.Error(), ShouldBeNil)
	})

	Convey("Given a staticChordSource that produces an error", t, func() {
		err := errors.New("source failed")
		src := &staticChordSource{
			samples: [][]data.Chord{{data.BaseChord('x')}},
			err:     err,
		}
		So(src.Next(), ShouldBeTrue)
		So(src.Chords(), ShouldHaveLength, 1)
		So(src.Error(), ShouldEqual, err)
	})
}

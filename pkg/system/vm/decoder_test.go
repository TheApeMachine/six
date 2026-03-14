package vm

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
testDecoder implements vm.Decoder by mapping single-byte BaseChords back to bytes.
Used for unit testing the Decoder interface.
*/
type testDecoder struct{}

func (d *testDecoder) Decode(chords []data.Chord) [][]byte {
	out := make([][]byte, 0, len(chords))

	for _, chord := range chords {
		if chord.ActiveCount() == 0 {
			continue
		}

		for byteVal := 0; byteVal < 256; byteVal++ {
			base := data.BaseChord(byte(byteVal))
			if data.ChordSimilarity(&chord, &base) == chord.ActiveCount() {
				out = append(out, []byte{byte(byteVal)})
				break
			}
		}
	}

	return out
}

func TestDecoderEmptyInput(t *testing.T) {
	dec := &testDecoder{}
	Convey("Given empty chord input", t, func() {
		result := dec.Decode(nil)
		So(result, ShouldBeNil)
	})
	Convey("Given empty chord slice", t, func() {
		result := dec.Decode([]data.Chord{})
		So(len(result), ShouldEqual, 0)
	})
}

func TestDecoderNormalInput(t *testing.T) {
	dec := &testDecoder{}
	Convey("Given single-byte chords", t, func() {
		chords := []data.Chord{
			data.BaseChord('h'),
			data.BaseChord('i'),
		}
		result := dec.Decode(chords)
		So(len(result), ShouldEqual, 2)
		So(string(result[0]), ShouldEqual, "h")
		So(string(result[1]), ShouldEqual, "i")
	})
}

func TestDecoderMultiByteChord(t *testing.T) {
	Convey("Given chord built from multiple bytes", t, func() {
		chord, err := data.BuildChord([]byte("ab"))
		So(err, ShouldBeNil)
		So(chord.ActiveCount(), ShouldBeGreaterThan, 0)

		dec := &testDecoder{}
		result := dec.Decode([]data.Chord{chord})
		So(len(result), ShouldBeLessThanOrEqualTo, 1)
	})
}

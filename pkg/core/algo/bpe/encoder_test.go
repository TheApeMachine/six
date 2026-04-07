package bpe

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestEncoderEncode(t *testing.T) {
	t.Parallel()

	// Encode uses strings.Split; "" becomes a single empty token.
	Convey("Given an encoder", t, func() {
		Convey("when input contains spaces, It should split on spaces", func() {
			encoder := NewEncoder()
			tokens := encoder.Encode("aa bb aa")

			So(tokens, ShouldResemble, []string{"aa", "bb", "aa"})
		})

		Convey("when input is empty, It should return one empty token", func() {
			encoder := NewEncoder()
			tokens := encoder.Encode("")

			So(len(tokens), ShouldEqual, 1)
			So(tokens[0], ShouldEqual, "")
		})
	})
}

func BenchmarkEncoderEncode(b *testing.B) {
	encoder := NewEncoder()
	line := "the quick brown fox jumps over the lazy dog"

	b.ResetTimer()

	for b.Loop() {
		_ = encoder.Encode(line)
	}
}

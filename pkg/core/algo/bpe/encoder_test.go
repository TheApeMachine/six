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

func TestEncoderEncodeBytes(t *testing.T) {
	t.Parallel()

	Convey("Given an encoder", t, func() {
		Convey("EncodeBytes mirrors Encode on UTF-8 slab input", func() {
			encoder := NewEncoder()

			a := encoder.Encode("aa bb aa")
			b := encoder.EncodeBytes([]byte("aa bb aa"))

			So(b, ShouldResemble, a)
		})

		Convey("when slab is empty, It should match Encode empty string", func() {
			encoder := NewEncoder()

			So(encoder.EncodeBytes(nil), ShouldResemble, encoder.Encode(""))
			So(encoder.EncodeBytes([]byte{}), ShouldResemble, encoder.Encode(""))
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

func BenchmarkEncoderEncodeBytes(b *testing.B) {
	encoder := NewEncoder()
	slab := []byte("the quick brown fox jumps over the lazy dog")

	b.ResetTimer()

	for b.Loop() {
		_ = encoder.EncodeBytes(slab)
	}
}

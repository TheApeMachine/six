package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

func TestNewRotationToken(t *testing.T) {
	Convey("Given NewRotationToken", t, func() {
		Convey("When creating with RotationY and origin -1", func() {
			tok := NewRotationToken(geometry.RotationY, -1)

			Convey("It should have exactly 2 active bits", func() {
				So(tok.Chord.ActiveCount(), ShouldEqual, 2)
			})
			Convey("It should be detected as rotational", func() {
				So(tok.IsRotational(), ShouldBeTrue)
			})
			Convey("It should have LogicalFace 256", func() {
				So(tok.LogicalFace, ShouldEqual, 256)
			})
			Convey("It should have default TTL", func() {
				So(tok.TTL, ShouldEqual, defaultTTL)
			})
			Convey("DecodeRotation should roundtrip to RotationY", func() {
				decoded := tok.DecodeRotation()
				So(decoded.A, ShouldEqual, geometry.RotationY.A)
				So(decoded.B, ShouldEqual, geometry.RotationY.B)
			})
		})
	})
}

func TestNewDataToken(t *testing.T) {
	Convey("Given NewDataToken", t, func() {
		chord := data.BaseChord('X')
		Convey("When creating with chord and logical face 88", func() {
			tok := NewDataToken(chord, 88, -1)

			Convey("It should preserve the chord", func() {
				So(tok.Chord, ShouldResemble, chord)
			})
			Convey("It should not be rotational", func() {
				So(tok.IsRotational(), ShouldBeFalse)
			})
			Convey("It should have the specified LogicalFace", func() {
				So(tok.LogicalFace, ShouldEqual, 88)
			})
		})
	})
}

func TestTokenIsRotational(t *testing.T) {
	Convey("Given tokens of various types", t, func() {
		Convey("When the chord has exactly 2 active bits, it should be rotational", func() {
			var c data.Chord
			c.Set(1)
			c.Set(3)
			tok := Token{Chord: c, LogicalFace: 256}
			So(tok.IsRotational(), ShouldBeTrue)
		})
		Convey("When the chord has 5 active bits (base chord), it should NOT be rotational", func() {
			bc := data.BaseChord(65)
			tok := NewDataToken(bc, 65, 0)
			So(tok.IsRotational(), ShouldBeFalse)
		})
		Convey("When the chord has 1 active bit, it should NOT be rotational", func() {
			var c data.Chord
			c.Set(10)
			tok := Token{Chord: c}
			So(tok.IsRotational(), ShouldBeFalse)
		})
	})
}

func TestTokenDecodeRotation(t *testing.T) {
	Convey("Given rotation tokens", t, func() {
		Convey("When encoding IdentityRotation, A must be 1", func() {
			id := geometry.IdentityRotation()
			tok := NewRotationToken(id, 0)
			decoded := tok.DecodeRotation()
			So(decoded.A, ShouldEqual, 1)
			So(decoded.B, ShouldEqual, 0)
		})
		Convey("When encoding RotationX, DecodeRotation should recover it", func() {
			rot := geometry.RotationX
			tok := NewRotationToken(rot, 0)
			decoded := tok.DecodeRotation()
			So(decoded.A, ShouldEqual, rot.A)
			So(decoded.B, ShouldEqual, rot.B)
		})
		Convey("When encoding RotationZ, DecodeRotation should recover it", func() {
			rot := geometry.RotationZ
			tok := NewRotationToken(rot, 0)
			decoded := tok.DecodeRotation()
			So(decoded.A, ShouldEqual, rot.A)
			So(decoded.B, ShouldEqual, rot.B)
		})
		Convey("When chord is degenerate (1 bit), DecodeRotation should return IdentityRotation", func() {
			var c data.Chord
			c.Set(5)
			tok := Token{Chord: c}
			decoded := tok.DecodeRotation()
			So(decoded, ShouldResemble, geometry.IdentityRotation())
		})
	})
}

func BenchmarkNewRotationToken(b *testing.B) {
	rot := geometry.RotationY
	for range b.N {
		_ = NewRotationToken(rot, -1)
	}
}

func BenchmarkNewDataToken(b *testing.B) {
	chord := data.BaseChord('A')
	for range b.N {
		_ = NewDataToken(chord, 65, -1)
	}
}

func BenchmarkTokenIsRotational(b *testing.B) {
	tok := NewRotationToken(geometry.RotationY, -1)
	b.ResetTimer()
	for range b.N {
		_ = tok.IsRotational()
	}
}

func BenchmarkTokenDecodeRotation(b *testing.B) {
	tok := NewRotationToken(geometry.RotationY, -1)
	b.ResetTimer()
	for range b.N {
		_ = tok.DecodeRotation()
	}
}

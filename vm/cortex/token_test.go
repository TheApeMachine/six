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
			tok := NewRotationToken(geometry.DefaultRotTable.Y90, -1)

			Convey("It should have OpCompose", func() {
				So(tok.Op, ShouldEqual, OpCompose)
			})
			Convey("It should have the GF(257) carry", func() {
				So(tok.Carry.A, ShouldEqual, geometry.DefaultRotTable.Y90.A)
				So(tok.Carry.B, ShouldEqual, geometry.DefaultRotTable.Y90.B)
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
			Convey("It should have the specified LogicalFace", func() {
				So(tok.LogicalFace, ShouldEqual, 88)
			})
		})
	})
}

func BenchmarkNewRotationToken(b *testing.B) {
	rot := geometry.DefaultRotTable.Y90
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

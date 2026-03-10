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

func TestNewSignalToken(t *testing.T) {
	Convey("Given NewSignalToken", t, func() {
		dataChord := data.BaseChord('Q')
		control := data.Chord{}
		control.Set(256)
		signalChord := data.ChordOR(&dataChord, &control)
		mask := signalChord

		Convey("When creating a control-bearing signal", func() {
			tok := NewSignalToken(signalChord, mask, -1)

			Convey("It should remain in signal mode", func() {
				So(tok.IsSignal, ShouldBeTrue)
			})

			Convey("It should preserve the control-plane bit in SignalMask", func() {
				So(tok.SignalMask.Has(256), ShouldBeTrue)
			})

			Convey("It should preserve explicit mask constraints", func() {
				overlap := data.ChordAND(&tok.SignalMask, &mask)
				So(overlap.ActiveCount(), ShouldBeGreaterThan, 0)
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

func BenchmarkNewSignalToken(b *testing.B) {
	chord := data.Chord{}
	chord.Set(256)
	mask := chord

	for range b.N {
		_ = NewSignalToken(chord, mask, -1)
	}
}

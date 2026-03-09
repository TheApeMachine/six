package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

func TestArrive_RotationToken(t *testing.T) {
	Convey("Given a node with identity rotation", t, func() {
		node := NewNode(0, 0)

		Convey("When a RotationY token arrives", func() {
			tok := NewRotationToken(geometry.DefaultRotTable.Y90, -1)
			node.Arrive(tok)

			Convey("The node's lens should compose to RotationY", func() {
				So(node.Rot.A, ShouldEqual, geometry.DefaultRotTable.Y90.A)
				So(node.Rot.B, ShouldEqual, geometry.DefaultRotTable.Y90.B)
			})
		})
		Convey("When multiple rotation tokens arrive in sequence", func() {
			node.Arrive(NewRotationToken(geometry.DefaultRotTable.X90, -1))
			node.Arrive(NewRotationToken(geometry.DefaultRotTable.Y90, -1))

			Convey("The final rotation should be X composed with Y", func() {
				expected := geometry.DefaultRotTable.X90.Compose(geometry.DefaultRotTable.Y90)
				So(node.Rot.A, ShouldEqual, expected.A)
				So(node.Rot.B, ShouldEqual, expected.B)
			})
		})
	})
}

func TestArrive_DataToken(t *testing.T) {
	Convey("Given a node", t, func() {
		Convey("When a data token arrives at logical face 42", func() {
			node := NewNode(0, 0)
			chord := data.BaseChord(42)
			tok := NewDataToken(chord, 42, -1)
			tok.TTL = 5

			energyBefore := node.Energy()
			node.Arrive(tok)
			energyAfter := node.Energy()

			Convey("Energy should increase", func() {
				So(energyAfter, ShouldBeGreaterThan, energyBefore)
			})
			Convey("The chord should land on the routed physical face", func() {
				routed := node.Rot.Forward(42)
				c := node.Cube.Get(0, 0, routed)
				So(c.ActiveCount(), ShouldBeGreaterThan, 0)
			})
		})
		Convey("When LogicalFace is 256, it should be ignored", func() {
			node := NewNode(0, 0)
			var c data.Chord
			c.Set(1)
			c.Set(3)
			tok := Token{Chord: c, LogicalFace: 256, TTL: 5}
			node.Arrive(tok)
			So(node.traffic, ShouldEqual, 1)
		})
	})
}

func TestNodeHole(t *testing.T) {
	Convey("Given a node", t, func() {
		Convey("When the cube is empty", func() {
			node := NewNode(0, 0)
			anchor, hole, face, shouldDream := node.Hole()

			Convey("It should not dream", func() {
				So(shouldDream, ShouldBeFalse)
			})
			Convey("Face should be 256", func() {
				So(face, ShouldEqual, 256)
			})
			Convey("Anchor and hole should be zero", func() {
				So(anchor.ActiveCount(), ShouldEqual, 0)
				So(hole.ActiveCount(), ShouldEqual, 0)
			})
		})
		Convey("When the cube has a single dense face and a sparser face", func() {
			node := NewNode(0, 0)
			c20, c21 := data.BaseChord(20), data.BaseChord(21)
			peak := data.ChordOR(&c20, &c21)
			side := data.BaseChord(10)
			node.Cube.Set(0, 0, 20, peak)
			node.Cube.Set(0, 0, 10, side)

			anchor, hole, physicalFace, shouldDream := node.Hole()
			expectedSummary := node.CubeChord()
			expectedHole := data.ChordHole(&expectedSummary, &peak)

			Convey("Anchor should be the densest face chord", func() {
				So(anchor, ShouldResemble, peak)
			})
			Convey("Hole should be summary minus peak", func() {
				So(hole, ShouldResemble, expectedHole)
			})
			Convey("Physical face should be the densest", func() {
				So(physicalFace, ShouldEqual, 20)
			})
			Convey("It should dream when hole has structure and summary > peak", func() {
				So(shouldDream, ShouldBeTrue)
			})
		})
		Convey("When the cube has only one face with no deficit", func() {
			node := NewNode(0, 0)
			single := data.BaseChord(5)
			node.Cube.Set(0, 0, 5, single)

			_, _, _, shouldDream := node.Hole()
			Convey("It should not dream (hole would be empty)", func() {
				So(shouldDream, ShouldBeFalse)
			})
		})
	})
}

func BenchmarkArrive_DataToken(b *testing.B) {
	chord := data.BaseChord('A')
	tok := NewDataToken(chord, 65, -1)
	tok.TTL = 8

	b.ResetTimer()
	for range b.N {
		n := NewNode(0, 0)
		n.Arrive(tok)
	}
}

func BenchmarkArrive_RotationToken(b *testing.B) {
	tok := NewRotationToken(geometry.DefaultRotTable.Y90, -1)

	b.ResetTimer()
	for range b.N {
		n := NewNode(0, 0)
		n.Arrive(tok)
	}
}

func BenchmarkNodeHole(b *testing.B) {
	node := genNodeWithMultiFaceDensity(20, 8)
	b.ResetTimer()
	for range b.N {
		_, _, _, _ = node.Hole()
	}
}

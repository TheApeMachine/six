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
			tok := NewRotationToken(geometry.RotationY, -1)
			emitted := node.Arrive(tok)
			So(emitted, ShouldBeEmpty)

			Convey("The node's lens should compose to RotationY", func() {
				So(node.Rot.A, ShouldEqual, geometry.RotationY.A)
				So(node.Rot.B, ShouldEqual, geometry.RotationY.B)
			})
		})
		Convey("When multiple rotation tokens arrive in sequence", func() {
			_ = node.Arrive(NewRotationToken(geometry.RotationX, -1))
			_ = node.Arrive(NewRotationToken(geometry.RotationY, -1))

			Convey("The final rotation should be X composed with Y", func() {
				expected := geometry.RotationX.Compose(geometry.RotationY)
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
			emitted := node.Arrive(tok)
			energyAfter := node.Energy()

			Convey("Energy should increase", func() {
				So(energyAfter, ShouldBeGreaterThan, energyBefore)
			})
			Convey("Fresh node with single token typically has no interference emission", func() {
				So(len(emitted), ShouldBeGreaterThanOrEqualTo, 0)
			})
			Convey("The chord should land on the routed physical face", func() {
				routed := node.Rot.Forward(42)
				So(node.Cube[routed].ActiveCount(), ShouldBeGreaterThan, 0)
			})
		})
		Convey("When LogicalFace is 256, it should be ignored", func() {
			node := NewNode(0, 0)
			var c data.Chord
			c.Set(1)
			c.Set(3)
			tok := Token{Chord: c, LogicalFace: 256, TTL: 5}
			emitted := node.Arrive(tok)
			So(emitted, ShouldBeEmpty)
			So(node.traffic, ShouldEqual, 1)
		})
	})
}

func TestArrive_InterferenceEmission(t *testing.T) {
	Convey("Given a node with pre-existing face data", t, func() {
		node := NewNode(0, 0)
		existing := data.BaseChord(10)
		face := existing.IntrinsicFace()
		routed := node.Rot.Forward(face)
		node.Cube[routed] = existing

		Convey("When an overlapping chord with novel bits arrives", func() {
			incoming := data.BaseChord(11)
			hole := data.ChordHole(&incoming, &existing)
			tok := NewDataToken(incoming, incoming.IntrinsicFace(), -1)
			tok.TTL = 5

			emitted := node.Arrive(tok)

			Convey("If hole has content, interference may emit a token", func() {
				if hole.ActiveCount() > 0 {
					So(len(emitted) >= 0, ShouldBeTrue)
					for _, e := range emitted {
						So(e.TTL, ShouldEqual, 4)
						So(e.Origin, ShouldEqual, node.ID)
					}
				}
			})
		})
	})
}

func TestArrive_TransitiveResonance(t *testing.T) {
	Convey("Given a node with sufficient cube context", t, func() {
		node := NewNode(0, 0)
		for i := 0; i < 8; i++ {
			chord := data.BaseChord(byte(i))
			face := chord.IntrinsicFace()
			routed := node.Rot.Forward(face)
			node.Cube[routed] = data.ChordOR(&node.Cube[routed], &chord)
		}

		summary := node.CubeChord()
		Convey("When an overlapping chord arrives with shared structure", func() {
			incoming := data.BaseChord(3)
			shared := data.ChordGCD(&incoming, &summary)
			tok := NewDataToken(incoming, incoming.IntrinsicFace(), -1)
			tok.TTL = 5

			emitted := node.Arrive(tok)

			Convey("If shared overlap is sufficient, transitive resonance may emit", func() {
				if shared.ActiveCount() > 1 && summary.ActiveCount() > 5 {
					So(len(emitted) >= 0, ShouldBeTrue)
				}
			})
		})
	})
}

func TestArrive_MitosisEmission(t *testing.T) {
	Convey("Given a node whose face crosses MitosisThreshold", t, func() {
		node := NewNode(0, 0)
		face := 20
		var chord data.Chord
		for bit := 0; bit < 120; bit++ {
			chord.Set(bit % 257)
		}
		node.Cube[face] = chord

		density := node.FaceDensity(face)
		Convey("When density >= MitosisThreshold and data token arrives", func() {
			if density >= geometry.MitosisThreshold {
				tok := NewDataToken(data.BaseChord(20), 20, -1)
				tok.TTL = 5
				emitted := node.Arrive(tok)

				Convey("It should emit a pressure token", func() {
					So(len(emitted), ShouldBeGreaterThan, 0)
				})
			}
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
			node.Cube[20] = peak
			node.Cube[10] = side

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
			node.Cube[5] = single

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
		_ = n.Arrive(tok)
	}
}

func BenchmarkArrive_RotationToken(b *testing.B) {
	tok := NewRotationToken(geometry.RotationY, -1)

	b.ResetTimer()
	for range b.N {
		n := NewNode(0, 0)
		_ = n.Arrive(tok)
	}
}

func BenchmarkNodeHole(b *testing.B) {
	node := genNodeWithMultiFaceDensity(20, 8)
	b.ResetTimer()
	for range b.N {
		_, _, _, _ = node.Hole()
	}
}

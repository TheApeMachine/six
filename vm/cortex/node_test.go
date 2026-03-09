package cortex

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

// --- Test data generators ---
//
// Generators produce significant chord and node configurations rather than
// toy data. Dense nodes test energy, interference, and mitosis logic.

// genDenseChord produces a chord with n active bits using coprime spreading.
// Used to create nodes that approach mitosis threshold or exhibit interference.
func genDenseChord(seed int64, activeBits int) data.Chord {
	rng := rand.New(rand.NewSource(seed))
	primes := []int{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47}
	var chord data.Chord
	for i := 0; i < activeBits && i < len(primes); i++ {
		p := primes[rng.Intn(len(primes))]
		if p < 257 {
			chord.Set(p)
		}
	}

	return chord
}

// genNodeWithMultiFaceDensity populates a node with chords across multiple
// faces to simulate realistic accumulation patterns.
func genNodeWithMultiFaceDensity(faceCount int, densityPerFace int) *Node {
	node := NewNode(0, 0)
	for face := 0; face < faceCount && face < geometry.CubeFaces; face++ {
		chord := genDenseChord(int64(face*31), densityPerFace)
		if chord.ActiveCount() == 0 {
			chord = data.BaseChord(byte(face % 256))
		}
		node.Cube[face] = data.ChordOR(&node.Cube[face], &chord)
	}

	return node
}

func TestNewNode(t *testing.T) {
	Convey("Given NewNode", t, func() {
		Convey("When creating a node with id 42 and birth tick 7", func() {
			node := NewNode(42, 7)

			Convey("It should have identity rotation", func() {
				So(node.Rot, ShouldResemble, geometry.IdentityRotation())
			})
			Convey("It should have zero energy", func() {
				So(node.Energy(), ShouldEqual, 0)
			})
			Convey("It should preserve ID and birth", func() {
				So(node.ID, ShouldEqual, 42)
				So(node.birth, ShouldEqual, 7)
			})
			Convey("It should have non-nil inbox with capacity", func() {
				So(node.inbox, ShouldNotBeNil)
			})
		})
	})
}

func TestNodeConnect(t *testing.T) {
	Convey("Given two nodes", t, func() {
		nodeA := NewNode(0, 0)
		nodeB := NewNode(1, 0)

		Convey("When connecting A to B", func() {
			nodeA.Connect(nodeB)

			Convey("It should add an edge connecting A and B", func() {
				So(nodeA.EdgeCount(), ShouldEqual, 1)
				edge := nodeA.Edges()[0]
				connected := edge.A == nodeB || edge.B == nodeB
				So(connected, ShouldBeTrue)
			})
			Convey("When connecting A to B again, it should not duplicate", func() {
				nodeA.Connect(nodeB)
				So(nodeA.EdgeCount(), ShouldEqual, 1)
			})
		})
		Convey("When connecting A to itself, it should ignore", func() {
			nodeA.Connect(nodeA)
			So(nodeA.EdgeCount(), ShouldEqual, 0)
		})
		Convey("When connecting to nil, it should ignore", func() {
			nodeA.Connect(nil)
			So(nodeA.EdgeCount(), ShouldEqual, 0)
		})
	})
}

func TestNodeEnergy(t *testing.T) {
	Convey("Given a node", t, func() {
		Convey("When the cube is empty, Energy should return 0", func() {
			node := NewNode(0, 0)
			So(node.Energy(), ShouldEqual, 0)
		})
		Convey("When a face has data, Energy should increase proportionally", func() {
			node := NewNode(0, 0)
			chord := data.BaseChord(42)
			node.Cube[42] = chord

			energy := node.Energy()
			So(energy, ShouldBeGreaterThan, 0)
			So(energy, ShouldBeLessThanOrEqualTo, 1.0)
		})
		Convey("When multiple faces are populated, Energy should sum correctly", func() {
			node := genNodeWithMultiFaceDensity(10, 5)
			energy := node.Energy()
			So(energy, ShouldBeGreaterThan, 0)
			So(energy, ShouldBeLessThanOrEqualTo, 1.0)
		})
	})
}

func TestNodeFaceDensity(t *testing.T) {
	Convey("Given a node", t, func() {
		Convey("When a face is empty, FaceDensity should return 0", func() {
			node := NewNode(0, 0)
			So(node.FaceDensity(0), ShouldEqual, 0)
		})
		Convey("When a face has a base chord (5 bits), FaceDensity should reflect it", func() {
			node := NewNode(0, 0)
			chord := data.BaseChord(100)
			face := chord.IntrinsicFace()
			node.Cube[face] = chord

			density := node.FaceDensity(face)
			So(density, ShouldBeGreaterThan, 0)
			So(density, ShouldEqual, float64(chord.ActiveCount())/257.0)
		})
	})
}

func TestNodeSendAndDrainInbox(t *testing.T) {
	Convey("Given a node", t, func() {
		node := NewNode(0, 0)

		Convey("When Send is called with tokens", func() {
			tok1 := NewDataToken(data.BaseChord('A'), 65, -1)
			tok2 := NewDataToken(data.BaseChord('B'), 66, -1)
			node.Send(tok1)
			node.Send(tok2)

			Convey("DrainInbox should return them in order", func() {
				drained := node.DrainInbox()
				So(len(drained), ShouldEqual, 2)
				So(drained[0].LogicalFace, ShouldEqual, 65)
				So(drained[1].LogicalFace, ShouldEqual, 66)
			})
		})
		Convey("When DrainInbox is called on empty inbox", func() {
			drained := node.DrainInbox()
			So(drained, ShouldBeEmpty)
		})
	})
}

func TestNodeBestFace(t *testing.T) {
	Convey("Given a node", t, func() {
		Convey("When the cube is empty, BestFace should return 256 (delimiter)", func() {
			node := NewNode(0, 0)
			So(node.BestFace(), ShouldEqual, 256)
		})
		Convey("When a single face has data, BestFace should return its logical value", func() {
			node := NewNode(0, 0)
			chord := data.BaseChord('H')
			face := chord.IntrinsicFace()
			node.Cube[face] = chord

			So(node.BestFace(), ShouldEqual, int('H'))
		})
		Convey("When multiple faces have data, BestFace should return densest", func() {
			node := NewNode(0, 0)
			sparse := data.BaseChord(42)
			dense := data.BaseChord(100)
			d101 := data.BaseChord(101)
			d102 := data.BaseChord(102)
			dense = data.ChordOR(&dense, &d101)
			dense = data.ChordOR(&dense, &d102)

			node.Cube[42] = sparse
			node.Cube[100] = dense

			So(node.Cube[100].ActiveCount(), ShouldBeGreaterThan, node.Cube[42].ActiveCount())
			So(node.BestFace(), ShouldEqual, 100)
		})
		Convey("When rotation is applied, BestFace should decode via Reverse", func() {
			node := NewNode(0, 0)
			node.Rot = geometry.DefaultRotTable.Y90
			logicalH := int('H')
			physFace := node.Rot.Forward(logicalH)
			node.Cube[physFace] = data.BaseChord(byte(logicalH))

			So(node.BestFace(), ShouldEqual, logicalH)
		})
	})
}

func TestNodeCubeChord(t *testing.T) {
	Convey("Given a node with multiple face chords", t, func() {
		node := NewNode(0, 0)
		c1 := data.BaseChord(10)
		c2 := data.BaseChord(20)
		node.Cube[10] = c1
		node.Cube[20] = c2

		Convey("CubeChord should OR-fold all faces", func() {
			summary := node.CubeChord()
			expected := data.ChordOR(&c1, &c2)
			So(summary, ShouldResemble, expected)
		})
		Convey("CubeChord of empty node should be zero", func() {
			empty := NewNode(0, 0)
			summary := empty.CubeChord()
			So(summary.ActiveCount(), ShouldEqual, 0)
		})
	})
}

func BenchmarkNewNode(b *testing.B) {
	for range b.N {
		_ = NewNode(0, 0)
	}
}

func BenchmarkNodeEnergy(b *testing.B) {
	node := genNodeWithMultiFaceDensity(50, 8)
	b.ResetTimer()
	for range b.N {
		_ = node.Energy()
	}
}

func BenchmarkNodeBestFace(b *testing.B) {
	node := genNodeWithMultiFaceDensity(100, 5)
	b.ResetTimer()
	for range b.N {
		_ = node.BestFace()
	}
}

func BenchmarkNodeCubeChord(b *testing.B) {
	node := genNodeWithMultiFaceDensity(100, 5)
	b.ResetTimer()
	for range b.N {
		_ = node.CubeChord()
	}
}

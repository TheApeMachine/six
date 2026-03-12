package graph

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestASTNode_Print(t *testing.T) {
	Convey("Given an ASTNode", t, func() {
		node := &ASTNode{
			Level: 1,
			Label: data.Chord{}, // ActiveCount() == 0 naturally
			Theta: 1.57,
		}
		
		Convey("It prints without panicking", func() {
			So(func() { node.Print(">>") }, ShouldNotPanic)
		})
	})
}

func TestExtractSharedInvariant(t *testing.T) {
	Convey("Given sequences of chords", t, func() {
		seqA := func() []data.Chord {
			c1, _ := data.BuildChord([]byte("Apple"))
			c2, _ := data.BuildChord([]byte("Banana"))
			return []data.Chord{c1, c2}
		}()
		
		seqB := func() []data.Chord {
			c1, _ := data.BuildChord([]byte("Apple"))
			c2, _ := data.BuildChord([]byte("Carrot"))
			return []data.Chord{c1, c2}
		}()
		
		seqC := func() []data.Chord {
			c1, _ := data.BuildChord([]byte("Peach"))
			c2, _ := data.BuildChord([]byte("Apple"))
			return []data.Chord{c1, c2}
		}()

		Convey("When passed a list of sequences, it extracts the GCD (AND intersection)", func() {
			invariant := extractSharedInvariant([][]data.Chord{seqA, seqB, seqC})

			// The shared invariant among all should be the representation of "Apple"
			// But note that "Banana", "Carrot", and "Peach" may share incidental bytes, 
			// so the invariant will be slightly larger than exactly "Apple".
			expectedApple, _ := data.BuildChord([]byte("Apple"))
			
			// It must completely contain Apple.
			sim := data.ChordSimilarity(&invariant, &expectedApple)
			So(sim, ShouldEqual, expectedApple.ActiveCount())
			
			// And it should have bits intersecting all three target texts
			So(invariant.ActiveCount(), ShouldBeGreaterThanOrEqualTo, expectedApple.ActiveCount())
		})
		
		Convey("Empty inputs return zero chords", func() {
			invariant := extractSharedInvariant([][]data.Chord{})
			So(invariant.ActiveCount(), ShouldEqual, 0)
		})
	})
}

func TestXorSequence(t *testing.T) {
	Convey("Given a sequence and a label chord", t, func() {
		chord1, _ := data.BuildChord([]byte("The quick brown fox"))
		chord2, _ := data.BuildChord([]byte("jumps over the lazy dog"))
		seq := []data.Chord{chord1, chord2}
		
		label, _ := data.BuildChord([]byte("brown dog"))
		
		Convey("It computes the geometric residue correctly", func() {
			residue := xorSequence(seq, label)
			So(len(residue), ShouldEqual, 2)
			
			// They should have lost the information related to the label.
			// Specifically, chord1 XOR "brown dog" = (chord1 - "brown") since "brown" is inside.
			// Plus bits from "dog" added back (because XOR adds where absent).
			expected1 := chord1.XOR(label)
			expected2 := chord2.XOR(label)
			
			So(residue[0].ActiveCount(), ShouldEqual, expected1.ActiveCount())
			So(residue[1].ActiveCount(), ShouldEqual, expected2.ActiveCount())
		})
		
		Convey("A chord matching the label perfectly drops to zero and is filtered", func() {
			exactChord, _ := data.BuildChord([]byte("absolute match"))
			seqExact := []data.Chord{exactChord}
			
			residue := xorSequence(seqExact, exactChord)
			// Since active count will be 0, the function filters it out
			So(len(residue), ShouldEqual, 0)
		})
	})
}

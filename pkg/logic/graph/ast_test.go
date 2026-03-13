package graph

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/data"
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

		Convey("It prints with children without panicking", func() {
			parent := &ASTNode{
				Level: 0,
				Label: data.Chord{},
				Theta: 0.0,
				Children: []*ASTNode{node},
			}
			So(func() { parent.Print(">>") }, ShouldNotPanic)
		})
	})
}

func TestExtractSharedInvariant(t *testing.T) {
	Convey("Given sequences of chords", t, func() {
		seqA := func() []data.Chord {
			c1, err := data.BuildChord([]byte("Apple"))
			if err != nil { t.Fatalf("BuildChord failed: %v", err) }
			c2, err := data.BuildChord([]byte("Banana"))
			if err != nil { t.Fatalf("BuildChord failed: %v", err) }
			return []data.Chord{c1, c2}
		}()

		seqB := func() []data.Chord {
			c1, err := data.BuildChord([]byte("Apple"))
			if err != nil { t.Fatalf("BuildChord failed: %v", err) }
			c2, err := data.BuildChord([]byte("Carrot"))
			if err != nil { t.Fatalf("BuildChord failed: %v", err) }
			return []data.Chord{c1, c2}
		}()

		seqC := func() []data.Chord {
			c1, err := data.BuildChord([]byte("Peach"))
			if err != nil { t.Fatalf("BuildChord failed: %v", err) }
			c2, err := data.BuildChord([]byte("Apple"))
			if err != nil { t.Fatalf("BuildChord failed: %v", err) }
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
		chord1, err := data.BuildChord([]byte("The quick brown fox"))
		if err != nil { t.Fatalf("BuildChord failed: %v", err) }
		chord2, err := data.BuildChord([]byte("jumps over the lazy dog"))
		if err != nil { t.Fatalf("BuildChord failed: %v", err) }
		seq := []data.Chord{chord1, chord2}

		label, err := data.BuildChord([]byte("brown dog"))
		if err != nil { t.Fatalf("BuildChord failed: %v", err) }

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
			exactChord, err := data.BuildChord([]byte("absolute match"))
			if err != nil { t.Fatalf("BuildChord failed: %v", err) }
			seqExact := []data.Chord{exactChord}

			residue := xorSequence(seqExact, exactChord)
			// Since active count will be 0, the function filters it out
			So(len(residue), ShouldEqual, 0)
		})
	})
}

func BenchmarkExtractSharedInvariant(b *testing.B) {
	c1, _ := data.BuildChord([]byte("Common text A"))
	c2, _ := data.BuildChord([]byte("Common text B"))
	seqs := [][]data.Chord{{c1, c2}, {c1, c2}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractSharedInvariant(seqs)
	}
}

func BenchmarkXorSequence(b *testing.B) {
	chord1, _ := data.BuildChord([]byte("Hello world"))
	chord2, _ := data.BuildChord([]byte("Foo bar"))
	label, _ := data.BuildChord([]byte("shared value"))
	seq := []data.Chord{chord1, chord2}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		xorSequence(seq, label)
	}
}

func BenchmarkASTNode_Print(b *testing.B) {
	node := &ASTNode{
		Level: 1,
		Label: data.Chord{},
		Theta: 1.0,
		Children: []*ASTNode{
			{Level: 2, Theta: 2.0},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node.Print("")
	}
}

package substrate

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
)

func mustBuildValue(t *testing.T, input []byte) data.Value {
	t.Helper()
	value, err := data.BuildValue(input)
	if err != nil {
		t.Fatalf("BuildValue failed: %v", err)
	}
	return value
}

func TestExtractSharedInvariant(t *testing.T) {
	seqA := []data.Value{
		mustBuildValue(t, []byte("Apple")),
		mustBuildValue(t, []byte("Banana")),
	}
	seqB := []data.Value{
		mustBuildValue(t, []byte("Apple")),
		mustBuildValue(t, []byte("Carrot")),
	}
	seqC := []data.Value{
		mustBuildValue(t, []byte("Peach")),
		mustBuildValue(t, []byte("Apple")),
	}

	Convey("Given sequences of values", t, func() {
		Convey("When passed a list of sequences, it extracts the GCD (AND intersection)", func() {
			invariant := extractSharedInvariant([][]data.Value{seqA, seqB, seqC})

			// The shared invariant among all should be the representation of "Apple"
			// But note that "Banana", "Carrot", and "Peach" may share incidental bytes,
			// so the invariant will be slightly larger than exactly "Apple".
			expectedApple := mustBuildValue(t, []byte("Apple"))

			// It must completely contain Apple.
			sim := data.ValueSimilarity(&invariant, &expectedApple)
			So(sim, ShouldEqual, expectedApple.ActiveCount())

			// And it should have bits intersecting all three target texts
			So(invariant.ActiveCount(), ShouldBeGreaterThanOrEqualTo, expectedApple.ActiveCount())
		})

		Convey("Empty inputs return zero values", func() {
			invariant := extractSharedInvariant([][]data.Value{})
			So(invariant.ActiveCount(), ShouldEqual, 0)
		})
	})
}

func TestXorSequence(t *testing.T) {
	Convey("Given a sequence and a label value", t, func() {
		value1, err := data.BuildValue([]byte("The quick brown fox"))
		if err != nil {
			t.Fatalf("BuildValue failed: %v", err)
		}
		value2, err := data.BuildValue([]byte("jumps over the lazy dog"))
		if err != nil {
			t.Fatalf("BuildValue failed: %v", err)
		}
		seq := []data.Value{value1, value2}

		label, err := data.BuildValue([]byte("brown dog"))
		if err != nil {
			t.Fatalf("BuildValue failed: %v", err)
		}

		Convey("It computes the geometric residue correctly", func() {
			residue := xorSequence(seq, label)
			So(len(residue), ShouldEqual, 2)

			// They should have lost the information related to the label.
			// Specifically, value1 XOR "brown dog" = (value1 - "brown") since "brown" is inside.
			// Plus bits from "dog" added back (because XOR adds where absent).
			expected1 := value1.XOR(label)
			expected2 := value2.XOR(label)

			So(residue[0].ActiveCount(), ShouldEqual, expected1.ActiveCount())
			So(residue[1].ActiveCount(), ShouldEqual, expected2.ActiveCount())
		})

		Convey("A value matching the label perfectly drops to zero and is filtered", func() {
			exactValue, err := data.BuildValue([]byte("absolute match"))
			if err != nil {
				t.Fatalf("BuildValue failed: %v", err)
			}
			seqExact := []data.Value{exactValue}

			residue := xorSequence(seqExact, exactValue)
			// Since active count will be 0, the function filters it out
			So(len(residue), ShouldEqual, 0)
		})
	})
}

func BenchmarkExtractSharedInvariant(b *testing.B) {
	c1, _ := data.BuildValue([]byte("Common text A"))
	c2, _ := data.BuildValue([]byte("Common text B"))
	seqs := [][]data.Value{{c1, c2}, {c1, c2}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractSharedInvariant(seqs)
	}
}

func BenchmarkXorSequence(b *testing.B) {
	value1, _ := data.BuildValue([]byte("Hello world"))
	value2, _ := data.BuildValue([]byte("Foo bar"))
	label, _ := data.BuildValue([]byte("shared value"))
	seq := []data.Value{value1, value2}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		xorSequence(seq, label)
	}
}

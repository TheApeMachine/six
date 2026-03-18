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

/*
valueEqual returns true when two Values are bitwise identical (XOR == 0).
*/
func valueEqual(left, right data.Value) bool {
	return left.XOR(right).ActiveCount() == 0
}

func TestExtractSharedInvariant(t *testing.T) {
	apple := mustBuildValue(t, []byte("Apple"))
	banana := mustBuildValue(t, []byte("Banana"))
	carrot := mustBuildValue(t, []byte("Carrot"))
	peach := mustBuildValue(t, []byte("Peach"))

	seqA := []data.Value{apple, banana}
	seqB := []data.Value{apple, carrot}
	seqC := []data.Value{peach, apple}

	Convey("Given sequences of values", t, func() {
		Convey("It should return the exact AND of the per-sequence OR unions", func() {
			unionA := apple.OR(banana)
			unionB := apple.OR(carrot)
			unionC := peach.OR(apple)
			expected := unionA.AND(unionB)
			expected = expected.AND(unionC)

			invariant := extractSharedInvariant([][]data.Value{seqA, seqB, seqC})

			So(valueEqual(invariant, expected), ShouldBeTrue)
			So(invariant.ActiveCount(), ShouldEqual, expected.ActiveCount())
		})

		Convey("Empty inputs return a zero-energy value", func() {
			invariant := extractSharedInvariant([][]data.Value{})
			So(invariant.ActiveCount(), ShouldEqual, 0)
		})

		Convey("A single sequence returns its own OR union unchanged", func() {
			expected := apple.OR(banana)
			invariant := extractSharedInvariant([][]data.Value{seqA})

			So(valueEqual(invariant, expected), ShouldBeTrue)
		})

		Convey("Two identical sequences return their shared OR union", func() {
			expected := apple.OR(banana)
			invariant := extractSharedInvariant([][]data.Value{seqA, seqA})

			So(valueEqual(invariant, expected), ShouldBeTrue)
		})
	})
}

func TestXorSequence(t *testing.T) {
	value1 := mustBuildValue(t, []byte("The quick brown fox"))
	value2 := mustBuildValue(t, []byte("jumps over the lazy dog"))
	label := mustBuildValue(t, []byte("brown dog"))

	Convey("Given a sequence and a label value", t, func() {
		Convey("Each residue should be the exact XOR of the element and the label", func() {
			seq := []data.Value{value1, value2}
			residue := xorSequence(seq, label)

			expected0 := value1.XOR(label)
			expected1 := value2.XOR(label)

			So(len(residue), ShouldEqual, 2)
			So(valueEqual(residue[0], expected0), ShouldBeTrue)
			So(valueEqual(residue[1], expected1), ShouldBeTrue)
		})

		Convey("A value XORed with itself produces zero and is filtered out", func() {
			residue := xorSequence([]data.Value{label}, label)
			So(len(residue), ShouldEqual, 0)
		})

		Convey("Mixed: elements that cancel vanish, non-cancelling survive as exact residues", func() {
			seq := []data.Value{label, value1}
			residue := xorSequence(seq, label)

			expected := value1.XOR(label)

			So(len(residue), ShouldEqual, 1)
			So(valueEqual(residue[0], expected), ShouldBeTrue)
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

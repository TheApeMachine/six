package cooccurrence

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewMatrix(t *testing.T) {
	t.Parallel()

	Convey("Given a window size", t, func() {
		matrix := NewMatrix(2)

		Convey("It should initialize with the correct window", func() {
			So(matrix.Window, ShouldEqual, 2)
		})

		Convey("It should have empty structures", func() {
			So(len(matrix.VocabularyOrder), ShouldEqual, 0)
			So(len(matrix.Counts), ShouldEqual, 0)
		})
	})
}

func TestMatrixUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update increments co-occurrence inside the window", t, func() {
		matrix := NewMatrix(1)
		matrix.Update([]string{"a", "b", "c"})

		So(matrix.Counts["a"]["b"], ShouldEqual, 1.0)
		So(matrix.Counts["b"]["a"], ShouldEqual, 1.0)
		So(matrix.Counts["b"]["c"], ShouldEqual, 1.0)
		So(matrix.VocabularyOrder, ShouldContain, "a")
	})

	Convey("Update deduplicates vocabulary order", t, func() {
		matrix := NewMatrix(1)
		matrix.Update([]string{"x", "x"})

		So(len(matrix.VocabularyOrder), ShouldEqual, 1)
	})
}

func BenchmarkMatrixUpdate(b *testing.B) {
	words := []string{"a", "b", "c", "d", "e"}
	matrix := NewMatrix(2)

	b.ResetTimer()

	for b.Loop() {
		matrix.Update(words)
	}
}

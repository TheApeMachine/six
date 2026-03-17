package sequencer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAnalyze(t *testing.T) {
	Convey("Given a Sequitur", t, func() {
		seq := NewSequitur()

		Convey("When analyzing bytes that form a repeated digram", func() {
			seq.Analyze(0, 'a')
			seq.Analyze(1, 'b')
			seq.Analyze(2, 'a')
			b, isBoundary := seq.Analyze(3, 'b')

			Convey("It should detect a boundary and pass through the byte", func() {
				So(b, ShouldEqual, 'b')
				So(isBoundary, ShouldBeTrue)
				So(seq.RuleCount(), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When analyzing bytes without repetition", func() {
			b, isBoundary := seq.Analyze(0, 'x')
			So(b, ShouldEqual, 'x')
			So(isBoundary, ShouldBeFalse)

			b, isBoundary = seq.Analyze(1, 'y')
			So(b, ShouldEqual, 'y')
			So(isBoundary, ShouldBeFalse)
		})
	})
}

func TestFlush(t *testing.T) {
	Convey("Given a Sequitur with pending bytes", t, func() {
		seq := NewSequitur()
		seq.Analyze(0, 'a')
		seq.Analyze(1, 'b')

		Convey("When flushing", func() {
			hasPending := seq.Flush()

			Convey("It should report pending bytes", func() {
				So(hasPending, ShouldBeTrue)
			})
		})
	})

	Convey("Given a Sequitur with no pending bytes", t, func() {
		seq := NewSequitur()
		seq.Analyze(0, 'a')
		seq.Analyze(1, 'b')
		seq.Analyze(2, 'a')
		seq.Analyze(3, 'b') // Boundary drains pending

		Convey("When flushing", func() {
			hasPending := seq.Flush()

			Convey("It should return no pending", func() {
				So(hasPending, ShouldBeFalse)
			})
		})
	})
}

func TestRuleCount(t *testing.T) {
	Convey("Given a Sequitur", t, func() {
		seq := NewSequitur()

		Convey("When no rules have been created", func() {
			count := seq.RuleCount()

			Convey("It should report zero rules", func() {
				So(count, ShouldEqual, 0)
			})
		})

		Convey("When a repeated digram creates a rule", func() {
			seq.Analyze(0, 'a')
			seq.Analyze(1, 'b')
			seq.Analyze(2, 'a')
			seq.Analyze(3, 'b')

			count := seq.RuleCount()

			Convey("It should report at least one rule", func() {
				So(count, ShouldBeGreaterThan, 0)
			})
		})
	})
}

func BenchmarkAnalyze(b *testing.B) {
	seq := NewSequitur()
	bytes := []byte("abcdefghijabcdefghij")

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		for idx, byteVal := range bytes {
			seq.Analyze(uint32(idx), byteVal)
		}
	}
}

func BenchmarkFlush(b *testing.B) {
	seq := NewSequitur()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		seq.Analyze(0, 'a')
		seq.Analyze(1, 'b')
		seq.Flush()
	}
}

func BenchmarkRuleCount(b *testing.B) {
	seq := NewSequitur()
	for idx, byteVal := range []byte("abababababababab") {
		seq.Analyze(uint32(idx), byteVal)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = seq.RuleCount()
	}
}

func TestAnalyzeApproximateDigram(t *testing.T) {
	Convey("Given a Sequitur with near-repeated digrams", t, func() {
		seq := NewSequitur()

		Convey("When two digrams differ by only one bit", func() {
			seq.Analyze(0, 'a')
			seq.Analyze(1, 'b')
			seq.Analyze(2, 'a')
			b, isBoundary := seq.Analyze(3, 'c') // b=0x62, c=0x63 differ by one bit

			Convey("It should still form an approximate structural rule", func() {
				So(b, ShouldEqual, 'c')
				So(isBoundary, ShouldBeTrue)
				So(seq.RuleCount(), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When two digrams are too far apart", func() {
			seq = NewSequitur()
			seq.Analyze(0, 'a')
			seq.Analyze(1, 'b')
			seq.Analyze(2, 'x')
			_, isBoundary := seq.Analyze(3, 'y')

			Convey("It should reject the lossy rule", func() {
				So(isBoundary, ShouldBeFalse)
				So(seq.RuleCount(), ShouldEqual, 0)
			})
		})
	})
}

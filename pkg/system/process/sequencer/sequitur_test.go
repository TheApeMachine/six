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
			boundary, emitK, _, meta := seq.Analyze(3, 'b')

			Convey("It should detect a boundary and emit a rule", func() {
				So(boundary, ShouldBeTrue)
				So(emitK, ShouldBeGreaterThan, 0)
				So(meta.ActiveCount(), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When analyzing bytes without repetition", func() {
			boundary, _, _, _ := seq.Analyze(0, 'x')
			So(boundary, ShouldBeFalse)

			boundary, _, _, _ = seq.Analyze(1, 'y')
			So(boundary, ShouldBeFalse)
		})
	})
}

func TestFlush(t *testing.T) {
	Convey("Given a Sequitur with pending bytes", t, func() {
		seq := NewSequitur()
		seq.Analyze(0, 'a')
		seq.Analyze(1, 'b')

		Convey("When flushing", func() {
			boundary, emitK, _, meta := seq.Flush()

			Convey("It should emit the pending bytes", func() {
				So(boundary, ShouldBeTrue)
				So(emitK, ShouldEqual, 2)
				So(meta.ActiveCount(), ShouldBeGreaterThanOrEqualTo, 0)
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
			boundary, _, _, _ := seq.Flush()

			Convey("It should return no boundary", func() {
				So(boundary, ShouldBeFalse)
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
			boundary, emitK, _, meta := seq.Analyze(3, 'c') // b=0x62, c=0x63 differ by one bit

			Convey("It should still form an approximate structural rule", func() {
				So(boundary, ShouldBeTrue)
				So(emitK, ShouldBeGreaterThan, 0)
				So(meta.ActiveCount(), ShouldBeGreaterThan, 0)
				So(seq.RuleCount(), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When two digrams are too far apart", func() {
			seq = NewSequitur()
			seq.Analyze(0, 'a')
			seq.Analyze(1, 'b')
			seq.Analyze(2, 'x')
			boundary, _, _, _ := seq.Analyze(3, 'y')

			Convey("It should reject the lossy rule", func() {
				So(boundary, ShouldBeFalse)
				So(seq.RuleCount(), ShouldEqual, 0)
			})
		})
	})
}



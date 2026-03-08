package tokenizer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDistributionCost(t *testing.T) {
	Convey("Given a distribution", t, func() {
		d := NewDistribution()

		Convey("A monotone stream should have zero cost (entropy)", func() {
			for i := 0; i < 100; i++ {
				d.Add('a')
			}
			// 100 * log(100) - (100 * log(100)) = 0
			So(d.Cost(), ShouldBeBetween, -0.00001, 0.00001)
		})

		Convey("A uniform stream should have maximum cost", func() {
			for i := 0; i < 256; i++ {
				d.Add(byte(i))
			}
			// entropy should be log(256)
			// cost = N * log(N) - sum(1 * log(1)) = 256 * log(256)
			expected := 256 * 5.545177444479562
			So(d.Cost(), ShouldBeBetween, expected-0.1, expected+0.1)
		})
	})
}

func TestBoundaryDetection(t *testing.T) {
	Convey("Given a Sequencer", t, func() {
		seq := NewSequencer(nil)

		Convey("A sudden change in byte distribution should trigger a boundary", func() {
			// First 20 bytes: all 'a'
			for i := 0; i < 20; i++ {
				seq.Analyze(i, 'a')
			}
			// Next 10 bytes: all 'b'
			// The boundary should trigger exactly where 'b' starts or shortly after
			fired := false
			splitAt := -1
			for i := 20; i < 30; i++ {
				reset, _ := seq.Analyze(i, 'b')
				if reset {
					fired = true
					splitAt = i
					break
				}
			}

			// We need a second boundary to trigger the emission of the first one.
			for i := 30; i < 60; i++ {
				reset, _ := seq.Analyze(i, 'c')
				if reset {
					fired = true
					splitAt = i
					break
				}
			}

			So(fired, ShouldBeTrue)
			Printf("\n  Split detected at pos: %d\n", splitAt)
			// We expect the split to happen around pos 20 (where 'b' starts),
			// but emission is delayed due to candidate buffering.
			So(splitAt, ShouldBeGreaterThanOrEqualTo, 20)
			So(splitAt, ShouldBeLessThanOrEqualTo, 60)
		})

		Convey("A continuous monotone stream should NEVER trigger a boundary", func() {
			for i := 0; i < 1000; i++ {
				reset, _ := seq.Analyze(i, 'x')
				So(reset, ShouldBeFalse)
			}
		})
	})
}

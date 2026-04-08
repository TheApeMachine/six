package kadabra

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/numeric"
)

func TestDigestCouple(t *testing.T) {
	t.Parallel()

	Convey("Couple matches numeric.SurprisalVelocityCouple on growth fields", t, func() {
		left := &Digest{SurprisalGrowth: 0.5}
		right := Digest{SurprisalGrowth: 0.4}

		So(
			left.Couple(right),
			ShouldAlmostEqual,
			numeric.SurprisalVelocityCouple(0.5, 0.4),
			1e-12,
		)
	})

	Convey("when magnitudes are below noise floor, It should return 0", t, func() {
		left := &Digest{SurprisalGrowth: 0.001}
		right := Digest{SurprisalGrowth: 0.001}

		So(left.Couple(right), ShouldEqual, 0)
	})
}

func BenchmarkDigestCouple(b *testing.B) {
	left := &Digest{SurprisalGrowth: 1.7}
	right := Digest{SurprisalGrowth: -0.4}

	b.ResetTimer()

	for b.Loop() {
		_ = left.Couple(right)
	}
}

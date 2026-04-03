package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestChunkedHolisticStrength(t *testing.T) {

	Convey("Identical token regions yield full strength", t, func() {
		tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
		baseIdx := core.Cfg.Value.Region.Tokens.Start

		var a, b Value
		for word := 0; word < tokenWords; word++ {
			idx := baseIdx + word
			if idx >= len(a) {
				break
			}

			a[idx] = uint64(word*17 + 3)
			b[idx] = a[idx]
		}

		strength, ok := ChunkedHolisticStrength(&a, &b, 512, 0.45)
		So(ok, ShouldBeTrue)
		So(strength, ShouldEqual, 1.0)
	})

	Convey("Random-unrelated regions usually fail the holistic gate", t, func() {
		tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
		baseIdx := core.Cfg.Value.Region.Tokens.Start

		var a, b Value
		for word := 0; word < tokenWords; word++ {
			idx := baseIdx + word
			if idx >= len(a) {
				break
			}

			a[idx] = ^uint64(word)
			b[idx] = uint64(word) * 13
		}

		_, ok := ChunkedHolisticStrength(&a, &b, 512, 0.05)
		So(ok, ShouldBeFalse)
	})
}

func BenchmarkChunkedHolisticStrength(bench *testing.B) {
	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	baseIdx := core.Cfg.Value.Region.Tokens.Start

	var left, right Value
	for word := 0; word < tokenWords; word++ {
		idx := baseIdx + word
		if idx >= len(left) {
			break
		}

		left[idx] = uint64(word)
		right[idx] = left[idx] ^ 1
	}

	chunk := core.Cfg.System.HolisticChunkBits
	maxDist := core.Cfg.System.HolisticHammingMax

	bench.ResetTimer()
	for bench.Loop() {
		s, matched := ChunkedHolisticStrength(&left, &right, chunk, maxDist)
		benchSink += s
		if matched {
			benchSink += 0.1
		}
	}
}

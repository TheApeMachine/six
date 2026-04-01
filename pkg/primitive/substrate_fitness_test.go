package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

// benchSink prevents the benchmark body from being optimized away.
var benchSink float64

func TestSubstrateExploitScore(t *testing.T) {
	Convey("Identical token regions yield a score of 1", t, func() {
		tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
		baseIdx := core.Cfg.Value.Region.Tokens.Start

		var parent, workspace Value
		parent[core.Cfg.Value.Region.ID.Start] = 1
		workspace[core.Cfg.Value.Region.ID.Start] = 2

		for w := 0; w < tokenWords; w++ {
			idx := baseIdx + w
			if idx >= core.Cfg.Value.Words {
				break
			}
			parent[idx] = 0xDEADBEEFCAFEBABE
			workspace[idx] = 0xDEADBEEFCAFEBABE
		}

		So(SubstrateExploitScore(&parent, &workspace), ShouldEqual, 1.0)
	})

	Convey("nil inputs yield 0", t, func() {
		var v Value
		So(SubstrateExploitScore(nil, &v), ShouldEqual, 0)
		So(SubstrateExploitScore(&v, nil), ShouldEqual, 0)
	})
}

func BenchmarkSubstrateExploitScore(b *testing.B) {
	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	baseIdx := core.Cfg.Value.Region.Tokens.Start

	var parent, workspace Value
	for w := 0; w < tokenWords; w++ {
		idx := baseIdx + w
		if idx >= core.Cfg.Value.Words {
			break
		}
		parent[idx] = ^uint64(w)
		workspace[idx] = uint64(w) * 7
	}

	b.ResetTimer()
	for b.Loop() {
		benchSink += SubstrateExploitScore(&parent, &workspace)
	}
}

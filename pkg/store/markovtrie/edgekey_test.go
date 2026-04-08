package markovtrie

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func setupMarkovTrieValueConfig(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
}

func TestTrieEdgeKey(t *testing.T) {
	t.Parallel()
	setupMarkovTrieValueConfig(t)

	Convey("trieEdgeKey matches String for token payload", t, func() {
		value, err := primitive.NewValue([]byte("tok"))

		So(err, ShouldBeNil)

		defer value.Close()

		stack := *value

		So(trieEdgeKey(stack), ShouldEqual, stack.String())
	})

	Convey("empty token region yields empty key", t, func() {
		var zero primitive.Value

		So(trieEdgeKey(zero), ShouldEqual, "")
	})
}

func BenchmarkTrieEdgeKey(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	value, err := primitive.NewValue([]byte("benchmark-token"))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	stack := *value

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = trieEdgeKey(stack)
	}
}

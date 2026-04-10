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
	setupMarkovTrieValueConfig(t)

	Convey("Given a Value with token payload", t, func() {
		value, err := primitive.FirstSegment(primitive.NewValue([]byte("tok")))

		So(err, ShouldBeNil)

		defer value.Close()

		stack := *value

		Convey("It should produce a non-empty Morton-coded key", func() {
			key := trieEdgeKey(stack)

			So(len(key), ShouldBeGreaterThan, 0)
		})

		Convey("It should be deterministic", func() {
			key1 := trieEdgeKey(stack)
			key2 := trieEdgeKey(stack)

			So(key1, ShouldEqual, key2)
		})

		Convey("It should roundtrip through String", func() {
			So(stack.String(), ShouldEqual, "tok")
		})
	})

	Convey("Given an empty token region", t, func() {
		var zero primitive.Value

		Convey("It should yield empty key", func() {
			So(trieEdgeKey(zero), ShouldEqual, "")
		})
	})
}

func BenchmarkTrieEdgeKey(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	value, err := primitive.FirstSegment(primitive.NewValue([]byte("benchmark-token")))

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

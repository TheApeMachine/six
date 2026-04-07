package episodic

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewBuffer(t *testing.T) {
	t.Parallel()

	Convey("NewBuffer returns nil for non-positive capacity", t, func() {
		So(NewBuffer(0, 0.5, 3, 0.1, 0.9), ShouldBeNil)
	})

	Convey("NewBuffer allocates ring storage", t, func() {
		buffer := NewBuffer(4, 0.5, 3, 0.1, 0.9)

		So(buffer, ShouldNotBeNil)
		So(buffer.Empty(), ShouldBeTrue)
	})
}

func TestBufferPush(t *testing.T) {
	t.Parallel()

	Convey("Push respects capacity and assigns monotonic IDs", t, func() {
		buffer := NewBuffer(2, 0.5, 3, 0.1, 0.9)

		buffer.Push([]string{"a"}, "L", 1)
		buffer.Push([]string{"b"}, "L", 2)
		buffer.Push([]string{"c"}, "L", 3)

		So(buffer.Len(), ShouldEqual, 2)
		So(buffer.events[0].Tokens[0], ShouldEqual, "b")
		So(buffer.events[1].Tokens[0], ShouldEqual, "c")
	})
}

func TestBufferEmpty(t *testing.T) {
	t.Parallel()

	Convey("Empty is true for nil buffer", t, func() {
		var buffer *Buffer

		So(buffer.Empty(), ShouldBeTrue)
	})
}

func TestBufferSnapshot(t *testing.T) {
	t.Parallel()

	Convey("Snapshot copies events and applies filter", t, func() {
		buffer := NewBuffer(8, 0.5, 3, 0.1, 0.9)

		buffer.Push([]string{"a", "b"}, "L", 1)

		out := buffer.Snapshot(func(tokens []string) []string {
			if len(tokens) == 0 {
				return tokens
			}

			return tokens[:1]
		})

		So(len(out), ShouldEqual, 1)
		So(out[0].Tokens, ShouldResemble, []string{"a"})
	})
}

func TestBufferNextDistribution(t *testing.T) {
	t.Parallel()

	Convey("NextDistribution matches suffix and respects label filter", t, func() {
		buffer := NewBuffer(8, 0.5, 10, 0.0, 0.0)

		buffer.Push([]string{"x", "y", "z"}, "A", 1)
		buffer.Push([]string{"x", "y", "q"}, "A", 2)

		dist := buffer.NextDistribution([]string{"x", "y"}, "A")

		So(dist["z"]+dist["q"], ShouldAlmostEqual, 1.0, 1e-9)
	})
}

func TestBufferBlend(t *testing.T) {
	t.Parallel()

	Convey("Blend returns trie map when episodic is empty", t, func() {
		buffer := NewBuffer(4, 0.5, 3, 0.1, 0.9)
		trie := map[string]float64{"a": 1.0}

		So(buffer.Blend(nil, "", trie, 0.5), ShouldEqual, trie)
	})

	Convey("Blend merges trie and episodic mass", t, func() {
		buffer := NewBuffer(8, 0.5, 10, 0.0, 0.0)

		buffer.Push([]string{"pref", "next"}, "L", 1)

		trie := map[string]float64{"next": 1.0}
		merged := buffer.Blend([]string{"pref"}, "L", trie, 0.5)

		So(len(merged), ShouldEqual, 1)
		So(merged["next"], ShouldAlmostEqual, 1.0, 1e-9)
	})
}

func TestBufferPickRandom(t *testing.T) {
	t.Parallel()

	Convey("PickRandom returns nil when empty", t, func() {
		buffer := NewBuffer(4, 0.5, 3, 0.1, 0.9)

		So(buffer.PickRandom(0), ShouldBeNil)
	})
}

func TestBufferLen(t *testing.T) {
	t.Parallel()

	Convey("Len on nil buffer is zero", t, func() {
		var buffer *Buffer

		So(buffer.Len(), ShouldEqual, 0)
	})
}

func BenchmarkBufferNextDistribution(b *testing.B) {
	buffer := NewBuffer(256, 0.5, 20, 0.0, 0.95)

	for range 200 {
		buffer.Push([]string{"a", "b", "c", "d"}, "L", buffer.Len())
	}

	ctx := []string{"a", "b"}
	label := "L"

	b.ResetTimer()

	for b.Loop() {
		_ = buffer.NextDistribution(ctx, label)
	}
}

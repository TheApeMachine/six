package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestValueComputeAffinityLSH(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("ComputeAffinityLSH fills affinity words from token bits", t, func() {
		tokens := randomTokenBytes()
		value, err := FirstSegment(NewValue(tokens[:]))

		So(err, ShouldBeNil)

		defer value.Close()

		So(value.ComputeAffinityLSH(), ShouldBeNil)

		aff := value.AffinityVector()

		So(aff, ShouldNotResemble, [AffinityWords]uint64{})
	})
}

func TestValueComputeAffinityLSHInvalidTokenBits(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("ComputeAffinityLSH errors when token bit width is zero", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		saved := core.Cfg.Value.Region.Tokens.Bits

		t.Cleanup(func() {
			core.Cfg.Value.Region.Tokens.Bits = saved
		})

		core.Cfg.Value.Region.Tokens.Bits = 0

		err := value.ComputeAffinityLSH()

		So(err, ShouldNotBeNil)
	})
}

func TestValueComputeAffinityFromContext(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("ComputeAffinityFromContext on nil returns nil", t, func() {
		var value *Value

		So(value.ComputeAffinityFromContext([]byte("x")), ShouldBeNil)
	})

	Convey("Empty context falls back to LSH", t, func() {
		value, err := FirstSegment(NewValue([]byte("ctx-empty")))

		So(err, ShouldBeNil)

		defer value.Close()

		before := value.AffinityVector()

		So(value.ComputeAffinityFromContext(nil), ShouldBeNil)

		So(value.AffinityVector(), ShouldResemble, before)
	})

	Convey("Non-empty context sets OR-of ngram bits in affinity", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		ctx := []byte("abcdefghijklmnop")

		So(value.ComputeAffinityFromContext(ctx), ShouldBeNil)

		So(value.AffinityVector(), ShouldNotResemble, [AffinityWords]uint64{})
	})
}

func TestComputeAffinityBloom(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("ComputeAffinityBloom ORs bit lanes for each 3-gram", t, func() {
		bloom := ComputeAffinityBloom([]byte("abcdef"))

		So(bloom, ShouldNotEqual, 0)
	})

	Convey("Short inputs reduce to a single fnv bit when non-empty", t, func() {
		So(ComputeAffinityBloom([]byte("ab")), ShouldNotEqual, 0)
	})

	Convey("Empty input yields zero bloom", t, func() {
		So(ComputeAffinityBloom(nil), ShouldEqual, 0)
	})
}

func TestFnvHash(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("fnvHash is stable for a fixed payload", t, func() {
		So(fnvHash([]byte("gamma")), ShouldEqual, fnvHash([]byte("gamma")))
		So(fnvHash([]byte("gamma")), ShouldNotEqual, fnvHash([]byte("delta")))
	})
}

func TestFnvBit(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("fnvBit always sets exactly one bit in the low 64 lanes", t, func() {
		mask := fnvBit([]byte("bit"))

		So(mask, ShouldNotEqual, 0)
		So(mask&(mask-1), ShouldEqual, 0)
	})
}

func TestBloomOverlap(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("BloomOverlap counts shared one-bits", t, func() {
		So(BloomOverlap(0x0f, 0xff), ShouldEqual, 4)
		So(BloomOverlap(0, 0), ShouldEqual, 0)
	})
}

func TestLFSRStep(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("LFSRStep rejects the all-zero trap state", t, func() {
		next := LFSRStep(0)

		So(next, ShouldNotEqual, 0)
		So(next, ShouldBeLessThanOrEqualTo, 0x1FFF)
	})
}

func TestLFSRAdvance(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("LFSRAdvance composes repeated LFSRStep", t, func() {
		seed := uint64(0x133)

		sequential := seed

		for range 10 {
			sequential = LFSRStep(sequential)
		}

		So(LFSRAdvance(seed, 10), ShouldEqual, sequential)
	})
}

func TestXORDistance(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("XORDistance is the bitwise XOR", t, func() {
		So(XORDistance(0x0f, 0xf0), ShouldEqual, 0xff)
	})
}

func TestXORDistanceLog(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("XORDistanceLog reports the highest differing bit index", t, func() {
		So(XORDistanceLog(0, 0), ShouldEqual, -1)
		So(XORDistanceLog(8, 0), ShouldEqual, 3)
	})
}

func BenchmarkValueComputeAffinityLSH(b *testing.B) {
	setupPrimitiveValueTest(b)

	value := newValueFromZeroFrame(b)

	defer value.Close()

	b.ResetTimer()

	for b.Loop() {
		if err := value.ComputeAffinityLSH(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValueComputeAffinityFromContext(b *testing.B) {
	setupPrimitiveValueTest(b)

	value := newValueFromZeroFrame(b)

	defer value.Close()

	ctx := []byte("benchmark context for primitive vsa affinity from bytes")

	b.ResetTimer()

	for b.Loop() {
		if err := value.ComputeAffinityFromContext(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVSABloomThreeGram(b *testing.B) {
	setupPrimitiveValueTest(b)

	data := []byte("benchmark bloom filter ngram overlay path")

	b.ResetTimer()

	var sink uint64

	for b.Loop() {
		sink = ComputeAffinityBloom(data)
	}

	_ = sink
}

func BenchmarkBloomOverlap(b *testing.B) {
	left := uint64(0xdeadbeefcafebabe)
	right := uint64(0x0000ffff0000ffff)

	b.ResetTimer()

	var sink int

	for b.Loop() {
		sink = BloomOverlap(left, right)
	}

	_ = sink
}

func BenchmarkLFSRAdvance(b *testing.B) {
	seed := uint64(7)

	b.ResetTimer()

	var sink uint64

	for b.Loop() {
		sink = LFSRAdvance(seed, 64)
	}

	_ = sink
}

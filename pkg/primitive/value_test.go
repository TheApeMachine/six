package primitive

import (
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func setupPrimitiveValueTest(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024

	core.Cfg.Value.Region.Tokens.Start = 0
	core.Cfg.Value.Region.Tokens.Bits = 512
	core.Cfg.Value.Region.Program.Start = 8
	core.Cfg.Value.Region.Signals.Start = 16
	core.Cfg.Value.Region.Context.Start = 24
	core.Cfg.Value.Region.Gradient.Start = 32
	core.Cfg.Value.Region.Meta.Start = 40
	core.Cfg.Value.Region.ID.Start = 122
	core.Cfg.Value.Region.Affinity.Start = 123
	core.Cfg.Value.Region.Affinity.Bits = 257
}

/*
newValueFromZeroFrame builds a Value from an all-zero payload of length
Value.Bytes — for tests that overwrite token words after construction.
*/
func newValueFromZeroFrame(tb testing.TB) *Value {
	tb.Helper()

	payload := make([]byte, core.Cfg.Value.Bytes)
	value, err := NewValue(payload)

	if err != nil {
		tb.Fatal(err)
	}

	return value
}

func TestWritePreservesFrameIdentity(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a full wire frame, Write preserves ID and layout", t, func() {
		first, err := NewValue([]byte("hello-world"))

		So(err, ShouldBeNil)
		So(first, ShouldNotBeNil)

		defer first.Close()

		expectedID := first.ID()

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, readErr := first.Read(frame)

		So(readErr, ShouldEqual, io.EOF)

		raw := valuePool.Get()
		second := raw.(*Value)
		_, writeErr := second.Write(frame)

		So(writeErr, ShouldBeNil)

		defer second.Close()

		So(second.ID(), ShouldEqual, expectedID)
	})
}

func TestValueFromWireFrame(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a wire frame from Read", t, func() {
		first, err := NewValue([]byte("hello-wire-frame"))
		So(err, ShouldBeNil)
		defer first.Close()

		expectedID := first.ID()
		expectedAff := first.AffinityVector()

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, readErr := first.Read(frame)

		So(readErr, ShouldEqual, io.EOF)

		decoded, decErr := ValueFromWireFrame(frame)

		So(decErr, ShouldBeNil)
		So(decoded, ShouldNotBeNil)
		defer decoded.Close()

		So(decoded.ID(), ShouldEqual, expectedID)
		So(decoded.AffinityVector(), ShouldResemble, expectedAff)
	})
}

func TestNewValue(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given raw source bytes", t, func() {
		source := []byte("roy is in the kitchen")

		Convey("NewValue should load them into the literal token head", func() {
			value, err := NewValue(source)
			So(err, ShouldBeNil)
			So(value, ShouldNotBeNil)
			defer value.Close()

			So(value.ID(), ShouldNotEqual, 0)

			buf := make([]byte, core.Cfg.Value.Bytes)
			n, readErr := value.Read(buf)
			So(readErr, ShouldEqual, io.EOF)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
			So(buf[:len(source)], ShouldResemble, source)
		})

		Convey("String should trim trailing NUL padding in the token slab", func() {
			short := []byte("cat")
			value, err := NewValue(short)
			So(err, ShouldBeNil)
			defer value.Close()

			So(len(value.String()), ShouldEqual, len(short))
			So(value.String(), ShouldEqual, "cat")
		})

		Convey("TokenRegionBytes should match String as UTF-8 payload", func() {
			short := []byte("cat")
			value, err := NewValue(short)
			So(err, ShouldBeNil)
			defer value.Close()

			So(string(value.TokenRegionBytes()), ShouldEqual, value.String())
		})
	})
}

func TestRead(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a populated Value", t, func() {
		source := []byte("roy is in the kitchen")
		value, err := NewValue(source)
		So(err, ShouldBeNil)
		defer value.Close()

		Convey("Read should serialize the full frame without copying semantics into higher layers", func() {
			buffer := make([]byte, core.Cfg.Value.Bytes)
			n, err := value.Read(buffer)

			So(err, ShouldEqual, io.EOF)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
			So(buffer[:len(source)], ShouldResemble, source)
		})
	})
}

func TestWrite(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a serialized Value frame", t, func() {
		source := []byte("roy is in the kitchen")
		src, err := NewValue(source)
		So(err, ShouldBeNil)
		defer src.Close()

		buffer := make([]byte, core.Cfg.Value.Bytes)
		_, err = src.Read(buffer)
		So(err, ShouldEqual, io.EOF)
	})
}

func TestClose(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given a populated Value", t, func() {
		value, err := NewValue([]byte("roy is in the kitchen"))
		So(err, ShouldBeNil)
		So(value, ShouldNotBeNil)

		Convey("Close should wipe the frame before returning it to the pool", func() {
			err := value.Close()

			So(err, ShouldBeNil)
			So(*value, ShouldResemble, Value{})
		})
	})
}

func TestNewValueEmptyPayload(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("NewValue rejects an empty payload", t, func() {
		value, err := NewValue(nil)

		So(err, ShouldEqual, io.ErrShortBuffer)
		So(value, ShouldBeNil)
	})
}

func TestValueFromWireFrameShortBuffer(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("ValueFromWireFrame requires a full frame", t, func() {
		value, err := ValueFromWireFrame(make([]byte, 16))

		So(err, ShouldEqual, io.ErrShortBuffer)
		So(value, ShouldBeNil)
	})
}

func TestValueReadShortBuffer(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Read requires the full configured byte width", t, func() {
		value, err := NewValue([]byte("short-read-test"))

		So(err, ShouldBeNil)

		defer value.Close()

		small := make([]byte, 8)
		n, readErr := value.Read(small)

		So(readErr, ShouldEqual, io.ErrShortBuffer)
		So(n, ShouldEqual, 0)
	})
}

func TestValueWriteShortBuffer(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Write rejects undersized frames", t, func() {
		value, err := NewValue([]byte("write-short"))

		So(err, ShouldBeNil)

		defer value.Close()

		n, writeErr := value.Write(make([]byte, 4))

		So(writeErr, ShouldEqual, io.ErrShortBuffer)
		So(n, ShouldEqual, 0)
	})
}

func TestValueCloseNil(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Close on nil is a no-op", t, func() {
		var value *Value

		So(value.Close(), ShouldBeNil)
	})
}

func TestValueSet(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Given Set on a live Value", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		value.Set(41, 0xabc)

		So((*value)[41], ShouldEqual, 0xabc)
	})

	Convey("Set ignores out-of-range indices and nil receivers", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		So(func() { value.Set(-1, 9) }, ShouldNotPanic)

		var nilValue *Value

		So(func() { nilValue.Set(0, 1) }, ShouldNotPanic)
	})
}

func TestValueID(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("ID reads the configured region start", t, func() {
		value, err := NewValue([]byte("id-path"))

		So(err, ShouldBeNil)

		defer value.Close()

		value.Set(core.Cfg.Value.Region.ID.Start, 4242)

		So(value.ID(), ShouldEqual, 4242)
	})

	Convey("ID on nil returns zero", t, func() {
		var value *Value

		So(value.ID(), ShouldEqual, 0)
	})
}

func TestValueAffinityVector(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("SetAffinityVector round-trips through AffinityVector with last-word mask", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		var aff [AffinityWords]uint64

		aff[0] = 0xfeed
		aff[AffinityWords-1] = ^uint64(0)

		value.SetAffinityVector(aff)

		back := value.AffinityVector()

		So(back[0], ShouldEqual, 0xfeed)
		So(back[AffinityWords-1], ShouldEqual, AffinityLastWordMask&aff[AffinityWords-1])
	})

	Convey("AffinityVector on nil returns zeros", t, func() {
		var value *Value

		So(value.AffinityVector(), ShouldResemble, [AffinityWords]uint64{})
	})
}

func TestValueContextVector(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("ContextVector reflects SetContextVector", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		var ctx [RegionWords]uint64

		ctx[0] = 0x101
		ctx[RegionWords-1] = 0x909

		value.SetContextVector(ctx)

		So(value.ContextVector(), ShouldResemble, ctx)
	})
}

func TestValueBindContext(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("BindContext is self-inverse under XOR", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		var bind [AffinityWords]uint64

		bind[0] = 0x55
		bind[1] = 0xaa

		before := value.ContextVector()

		value.BindContext(bind)
		mid := value.ContextVector()

		value.BindContext(bind)
		after := value.ContextVector()

		So(mid, ShouldNotResemble, before)
		So(after, ShouldResemble, before)
	})
}

func TestValueGradientVector(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("AccumulateGradient XORs into the gradient slab", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		var delta [RegionWords]uint64

		delta[0] = 0xf0f0
		delta[1] = 0x0f0f

		value.AccumulateGradient(delta)
		value.AccumulateGradient(delta)

		So(value.GradientVector(), ShouldResemble, [RegionWords]uint64{})
	})
}

func TestValueMetaWord(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("MetaWord and SetMetaWord address the meta region", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		value.SetMetaWord(MetaConfidence, 42)
		value.IncrementMeta(MetaConfidence)

		So(value.MetaWord(MetaConfidence), ShouldEqual, 43)
	})

	Convey("MetaWord clamps invalid offsets", t, func() {
		value := newValueFromZeroFrame(t)

		defer value.Close()

		So(value.MetaWord(-1), ShouldEqual, 0)
		So(value.MetaWord(RegionWords), ShouldEqual, 0)

		var nilValue *Value

		So(nilValue.MetaWord(0), ShouldEqual, 0)
	})
}

func TestValueBytes(t *testing.T) {
	setupPrimitiveValueTest(t)

	Convey("Bytes spans the full configured wire size", t, func() {
		value, err := NewValue([]byte("bytes-len"))

		So(err, ShouldBeNil)

		defer value.Close()

		So(len(value.Bytes()), ShouldEqual, core.Cfg.Value.Bytes)
	})
}

func BenchmarkValue_Set(b *testing.B) {
	setupPrimitiveValueTest(b)

	value := newValueFromZeroFrame(b)

	defer value.Close()

	b.ResetTimer()

	for b.Loop() {
		value.Set(60, uint64(b.N)^0xf00d)
	}
}

func BenchmarkValue_AffinityVector(b *testing.B) {
	setupPrimitiveValueTest(b)

	value, err := NewValue([]byte("bench-aff-vector"))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	b.ResetTimer()

	for b.Loop() {
		_ = value.AffinityVector()
	}
}

func BenchmarkValue_BindContext(b *testing.B) {
	setupPrimitiveValueTest(b)

	value, err := NewValue([]byte("bench-bind"))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	var bind [AffinityWords]uint64

	bind[0] = 0x123456789abcdef0

	b.ResetTimer()

	for b.Loop() {
		value.BindContext(bind)
	}
}

func BenchmarkValue_NewValue(b *testing.B) {
	setupPrimitiveValueTest(b)

	payload := []byte("benchmark new primitive value mint")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		v, err := NewValue(payload)

		if err != nil {
			b.Fatal(err)
		}

		if err := v.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValue_String(b *testing.B) {
	setupPrimitiveValueTest(b)

	value, err := NewValue([]byte("string token repr benchmark"))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	b.ResetTimer()

	var sink string

	for b.Loop() {
		sink = value.String()
	}

	_ = sink
}

func BenchmarkValue_Read(b *testing.B) {
	setupPrimitiveValueTest(b)

	value, err := NewValue([]byte("roy is in the kitchen"))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	buffer := make([]byte, core.Cfg.Value.Bytes)
	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		n, err := value.Read(buffer)

		if n != core.Cfg.Value.Bytes || err != io.EOF {
			b.Fatalf("Read: n=%d err=%v", n, err)
		}
	}
}

func BenchmarkValue_Write(b *testing.B) {
	setupPrimitiveValueTest(b)

	value, err := NewValue(make([]byte, core.Cfg.Value.Bytes))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	payload := make([]byte, core.Cfg.Value.Bytes)

	for index := range payload {
		payload[index] = byte(index)
	}

	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		if _, err := value.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValueFromWireFrame(b *testing.B) {
	setupPrimitiveValueTest(b)

	source, err := NewValue([]byte("benchmark wire decode"))
	if err != nil {
		b.Fatal(err)
	}

	frame := make([]byte, core.Cfg.Value.Bytes)
	if _, readErr := source.Read(frame); readErr != io.EOF {
		b.Fatalf("Read: %v", readErr)
	}

	if err := source.Close(); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(core.Cfg.Value.Bytes))
	b.ResetTimer()

	for b.Loop() {
		v, vErr := ValueFromWireFrame(frame)
		if vErr != nil {
			b.Fatal(vErr)
		}

		if err := v.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

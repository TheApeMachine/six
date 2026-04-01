package stepwise

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
)

func TestEncodeStep(t *testing.T) {
	convey.Convey("Given EncodeStep", t, func() {
		convey.Convey("When encoding XOR r0 r1 -> r2", func() {
			w := EncodeStep(0x6, 64, 65, 66)
			op, a, b, d, lb, rb, err := DecodeStep(w)

			convey.So(err, convey.ShouldBeNil)
			convey.So(op, convey.ShouldEqual, uint8(0x6))
			convey.So(a, convey.ShouldEqual, uint8(64))
			convey.So(b, convey.ShouldEqual, uint8(65))
			convey.So(d, convey.ShouldEqual, uint8(66))
			convey.So(lb, convey.ShouldBeFalse)
			convey.So(rb, convey.ShouldBeFalse)
		})
	})
}

func TestDecodeStep(t *testing.T) {
	convey.Convey("Given DecodeStep", t, func() {
		convey.Convey("When reserved bits 33+ are set", func() {
			w := uint64(1) << 33

			_, _, _, _, _, _, err := DecodeStep(w)

			convey.So(err, convey.ShouldNotBeNil)
		})
	})
}

func TestRunScalar(t *testing.T) {
	convey.Convey("Given RunScalar", t, func() {
		convey.Convey("When program XORs r0 and r1 into r2", func() {
			var ctx [FrameWords]uint64

			ctx[64] = 0xF0F0_F0F0_F0F0_F0F0
			ctx[65] = 0x0F0F_0F0F_0F0F_0F0F
			prog := []uint64{
				EncodeStep(0x6, 64, 65, 66),
			}

			err := RunScalar(&ctx, prog)

			convey.So(err, convey.ShouldBeNil)
			convey.So(ctx[66], convey.ShouldEqual, uint64(0xFFFF_FFFF_FFFF_FFFF))
		})

		convey.Convey("When padded with copy-noop steps", func() {
			var ctx [FrameWords]uint64

			ctx[64] = 0xDEADBEEF01234567
			prog := []uint64{
				EncodeStep(0x3, 64, 0, 67),
				EncodeStep(0x3, 67, 0, 67),
				EncodeStep(0x3, 67, 0, 67),
			}

			err := RunScalar(&ctx, prog)

			convey.So(err, convey.ShouldBeNil)
			convey.So(ctx[67], convey.ShouldEqual, ctx[64])
		})
	})
}

func TestRunEmbedded(t *testing.T) {
	convey.Convey("Given RunEmbedded", t, func() {
		convey.Convey("When descriptors fill the embedded program band", func() {
			var ctx [FrameWords]uint64

			base := EmbeddedProgramBase()
			n := EmbeddedStepCount()

			ctx[64] = 1
			ctx[65] = 2
			ctx[base] = PackEmbeddedHeader(uint16(n - 1))
			ctx[base+1] = EncodeStep(0x1, 64, 65, 66)
			for fill := 2; fill < n; fill++ {
				ctx[base+fill] = EncodeStep(0x3, 66, 66, 66)
			}

			err := RunEmbedded(&ctx)

			convey.So(err, convey.ShouldBeNil)
			convey.So(ctx[66], convey.ShouldEqual, uint64(0))
		})
	})
}

func TestRunHomogeneousBatch(t *testing.T) {
	convey.Convey("Given RunHomogeneousBatch", t, func() {
		convey.Convey("When batch matches RunScalar lane-by-lane", func() {
			prog := []uint64{
				EncodeStep(0x6, 64, 65, 66),
			}

			var a, b [FrameWords]uint64

			a[64] = 0xFF
			a[65] = 0x0F
			b[64] = 0x0F0F
			b[65] = 0xF00F

			_ = RunScalar(&a, prog)
			_ = RunScalar(&b, prog)

			var c, d [FrameWords]uint64

			c[64] = 0xFF
			c[65] = 0x0F
			d[64] = 0x0F0F
			d[65] = 0xF00F
			batch := []*[FrameWords]uint64{&c, &d}

			err := RunHomogeneousBatch(batch, prog)

			convey.So(err, convey.ShouldBeNil)
			convey.So(c[66], convey.ShouldEqual, a[66])
			convey.So(d[66], convey.ShouldEqual, b[66])
		})
	})
}

func TestCompileDescriptors(t *testing.T) {
	convey.Convey("Given CompileDescriptors", t, func() {
		convey.Convey("When source matches bootloader shape", func() {
			desc, err := CompileDescriptors("IMM 74 1\n")

			convey.So(err, convey.ShouldBeNil)
			convey.So(len(desc), convey.ShouldEqual, 1)
			convey.So(desc[0], convey.ShouldEqual, EncodeImm(74, 1))
		})
	})
}

func TestInstallEmbedded(t *testing.T) {
	convey.Convey("Given InstallEmbedded", t, func() {
		convey.Convey("It should write header and detect stepwise", func() {
			var v [FrameWords]uint64

			err := InstallEmbedded(&v, []uint64{EncodeImm(74, 1)})

			convey.So(err, convey.ShouldBeNil)
			convey.So(DetectEmbeddedStepwise(&v), convey.ShouldBeTrue)
			convey.So(v[74], convey.ShouldEqual, uint64(0))
			runErr := RunEmbeddedPair(&v, &v)
			convey.So(runErr, convey.ShouldBeNil)
			convey.So(v[74], convey.ShouldEqual, uint64(1))
		})
	})
}

func TestDetectEmbeddedStepwise(t *testing.T) {
	convey.Convey("Given DetectEmbeddedStepwise", t, func() {
		convey.Convey("When header magic is present", func() {
			var ctx [FrameWords]uint64

			ctx[EmbeddedProgramBase()] = PackEmbeddedHeader(3)

			convey.So(DetectEmbeddedStepwise(&ctx), convey.ShouldBeTrue)
		})

		convey.Convey("When first word is a legacy-looking opcode", func() {
			var ctx [FrameWords]uint64

			ctx[EmbeddedProgramBase()] = EncodeStep(0x1, 64, 65, 66)

			convey.So(DetectEmbeddedStepwise(&ctx), convey.ShouldBeFalse)
		})
	})
}

func TestRunPair(t *testing.T) {
	convey.Convey("Given RunPair", t, func() {
		convey.Convey("When XOR takes left from A and right from B", func() {
			var a, b [FrameWords]uint64

			a[64] = 0xFF00FF00FF00FF00
			b[64] = 0x0FF00FF00FF00FF0
			prog := []uint64{
				EncodeStepFrames(0x6, 64, 64, 66, false, true),
			}

			err := RunPair(&a, &b, prog)

			convey.So(err, convey.ShouldBeNil)
			convey.So(a[66], convey.ShouldEqual, cpu.ExecWord(0x6, a[64], b[64]))
		})
	})
}

func TestRunEmbeddedPair(t *testing.T) {
	convey.Convey("Given RunEmbeddedPair", t, func() {
		convey.Convey("When band encodes partner XOR into a sink register", func() {
			var a, b [FrameWords]uint64

			base := EmbeddedProgramBase()
			n := EmbeddedStepCount()

			a[64] = 0xAAAAAAAAAAAAAAAA
			b[64] = 0x5555555555555555
			a[base] = PackEmbeddedHeader(uint16(n - 1))
			a[base+1] = EncodeStepFrames(0x6, 64, 64, 66, false, true)
			for fill := 2; fill < n; fill++ {
				a[base+fill] = EncodeStep(0x3, 66, 66, 66)
			}

			err := RunEmbeddedPair(&a, &b)

			convey.So(err, convey.ShouldBeNil)
			convey.So(a[66], convey.ShouldEqual, uint64(0xFFFFFFFFFFFFFFFF))
		})
	})
}

func TestRunScalarExecWordAgreement(t *testing.T) {
	convey.Convey("Given RunScalar versus cpu.ExecWord", t, func() {
		convey.Convey("When every truth-table op touches random-ish lanes", func() {
			var ctx [FrameWords]uint64

			ctx[10] = 0xAAAAAAAAAAAAAAAA
			ctx[11] = 0x5555555555555555
			ctx[12] = 0

			for op := uint8(0x0); op <= 0xF; op++ {
				prog := []uint64{EncodeStep(op, 10, 11, 12)}

				err := RunScalar(&ctx, prog)

				convey.So(err, convey.ShouldBeNil)
				convey.So(ctx[12], convey.ShouldEqual, cpu.ExecWord(op, ctx[10], ctx[11]))
			}
		})
	})
}

func BenchmarkRunScalar(b *testing.B) {
	var ctx [FrameWords]uint64

	ctx[64] = 0xFF
	ctx[65] = 0x0F
	ctx[66] = 0

	prog := make([]uint64, 32)
	for i := range prog {
		prog[i] = EncodeStep(0x6, 64, 65, 66)
	}

	b.ResetTimer()

	for range b.N {
		_ = RunScalar(&ctx, prog)
	}
}

func BenchmarkRunHomogeneousBatch(b *testing.B) {
	const batchSize = 1024

	prog := make([]uint64, 32)
	for i := range prog {
		prog[i] = EncodeStep(0x6, 64, 65, 66)
	}

	contexts := make([]*[FrameWords]uint64, batchSize)
	frames := make([][FrameWords]uint64, batchSize)

	for lane := range frames {
		frames[lane][64] = uint64(lane + 1)
		frames[lane][65] = uint64(lane * 7)
		contexts[lane] = &frames[lane]
	}

	b.ResetTimer()

	for range b.N {
		_ = RunHomogeneousBatch(contexts, prog)
	}
}

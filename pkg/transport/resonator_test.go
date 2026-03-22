package transport

import (
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestResonatorRead(t *testing.T) {
	Convey("Given a Resonator anchored to a prompt", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(0)
		prompt.Set(2)
		prompt.Set(4)

		resonator := NewResonator(prompt)

		Convey("It should emit the prompt on first Read", func() {
			buf := make([]byte, primitive.ByteSize)
			n, err := resonator.Read(buf)

			So(n, ShouldEqual, primitive.ByteSize)
			So(err, ShouldBeNil)

			result := primitive.NewValueFromBytes(buf)
			So(result.Equal(prompt), ShouldBeTrue)
		})

		Convey("It should return EOF on second Read before any Write", func() {
			buf := make([]byte, primitive.ByteSize)
			resonator.Read(buf)

			n, err := resonator.Read(buf)

			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})
}

func TestResonatorConvergence(t *testing.T) {
	Convey("Given a Resonator that receives its own prompt as output", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(0)
		prompt.Set(2)
		prompt.Set(4)

		resonator := NewResonator(prompt)

		buf := make([]byte, primitive.ByteSize)
		resonator.Read(buf)

		Convey("It should converge when XOR residual is zero", func() {
			_, err := resonator.Write(buf)

			So(err, ShouldBeNil)
			So(resonator.Converged(), ShouldBeTrue)
		})

		Convey("It should return EOF on Read after convergence", func() {
			resonator.Write(buf)

			n, err := resonator.Read(buf)

			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})
}

func TestResonatorResidual(t *testing.T) {
	Convey("Given a Resonator that receives a partial match", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(0)
		prompt.Set(2)
		prompt.Set(4)

		resonator := NewResonator(prompt)

		buf := make([]byte, primitive.ByteSize)
		resonator.Read(buf)

		partial := primitive.NewValue()
		partial.Set(0)
		partial.Set(2)

		partialBuf := make([]byte, primitive.ByteSize)
		partial.Read(partialBuf)

		Convey("It should not converge", func() {
			_, err := resonator.Write(partialBuf)

			So(err, ShouldBeNil)
			So(resonator.Converged(), ShouldBeFalse)
		})

		Convey("It should emit the XOR residual on next Read", func() {
			resonator.Write(partialBuf)

			n, err := resonator.Read(buf)

			So(n, ShouldEqual, primitive.ByteSize)
			So(err, ShouldBeNil)

			residual := primitive.NewValueFromBytes(buf)
			So(residual.Has(4), ShouldBeTrue)
			So(residual.Has(0), ShouldBeFalse)
			So(residual.Has(2), ShouldBeFalse)
		})

		Convey("It should consume the residual after one Read", func() {
			resonator.Write(partialBuf)
			resonator.Read(buf)

			n, err := resonator.Read(buf)

			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})
}

func TestResonatorOrbitDetection(t *testing.T) {
	Convey("Given a Resonator that sees the same residual twice", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(0)
		prompt.Set(2)
		prompt.Set(4)

		resonator := NewResonator(prompt)

		buf := make([]byte, primitive.ByteSize)
		resonator.Read(buf)

		partial := primitive.NewValue()
		partial.Set(0)
		partial.Set(2)

		partialBuf := make([]byte, primitive.ByteSize)
		partial.Read(partialBuf)

		Convey("It should accept the first occurrence", func() {
			_, err := resonator.Write(partialBuf)

			So(err, ShouldBeNil)
		})

		Convey("It should return orbit error on the second occurrence", func() {
			resonator.Write(partialBuf)

			_, err := resonator.Write(partialBuf)

			So(err, ShouldEqual, ResonatorOrbitError)
		})
	})
}

func TestResonatorShortWrite(t *testing.T) {
	Convey("Given a Resonator receiving a short buffer", t, func() {
		prompt := primitive.NewValue()
		resonator := NewResonator(prompt)

		n, err := resonator.Write(make([]byte, 100))

		Convey("It should reject with ErrShortValue", func() {
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, primitive.ErrShortValue)
		})
	})
}

func TestResonatorTrace(t *testing.T) {
	Convey("Given a Resonator that has processed multiple outputs", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(0)
		prompt.Set(2)
		prompt.Set(4)

		resonator := NewResonator(prompt)

		buf := make([]byte, primitive.ByteSize)
		resonator.Read(buf)

		v1 := primitive.NewValue()
		v1.Set(0)
		v1.Set(2)

		v1Buf := make([]byte, primitive.ByteSize)
		v1.Read(v1Buf)
		resonator.Write(v1Buf)

		v2 := primitive.NewValue()
		v2.Set(0)
		v2.Set(2)
		v2.Set(4)
		v2.Set(7)

		v2Buf := make([]byte, primitive.ByteSize)
		v2.Read(v2Buf)

		resonator.Read(buf)
		resonator.Write(v2Buf)

		Convey("It should accumulate the trace in order", func() {
			trace := resonator.Trace()

			So(len(trace), ShouldEqual, 2)
			So(trace[0].Has(0), ShouldBeTrue)
			So(trace[0].Has(2), ShouldBeTrue)
			So(trace[0].Has(4), ShouldBeFalse)
			So(trace[1].Has(7), ShouldBeTrue)
		})
	})
}

func TestResonatorReify(t *testing.T) {
	Convey("Given a Resonator with an accumulated trace", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(0)
		prompt.Set(2)
		prompt.Set(4)

		resonator := NewResonator(prompt)

		buf := make([]byte, primitive.ByteSize)
		resonator.Read(buf)

		v1 := primitive.NewValue()
		v1.Set(10)
		v1.Set(20)
		v1.Set(30)

		v1Buf := make([]byte, primitive.ByteSize)
		v1.Read(v1Buf)
		resonator.Write(v1Buf)

		Convey("It should produce a non-zero tool Value", func() {
			tool := resonator.Reify()

			So(tool.PopCount(), ShouldBeGreaterThan, 0)
		})

		Convey("It should encode the composed motor's scale and translate as bit positions", func() {
			tool := resonator.Reify()
			scale, translate := v1.Motor()

			So(tool.Has(int(scale)), ShouldBeTrue)
			So(tool.Has(int(translate)), ShouldBeTrue)
		})
	})

	Convey("Given a Resonator with an empty trace", t, func() {
		prompt := primitive.NewValue()
		resonator := NewResonator(prompt)

		Convey("Reify should return a zero Value", func() {
			tool := resonator.Reify()

			So(tool.IsZero(), ShouldBeTrue)
		})
	})

	Convey("Given a Resonator with a multi-step trace", t, func() {
		prompt := primitive.NewValue()
		prompt.Set(0)

		resonator := NewResonator(prompt)

		buf := make([]byte, primitive.ByteSize)
		resonator.Read(buf)

		v1 := primitive.NewValue()
		v1.Set(5)
		v1.Set(11)

		v1Buf := make([]byte, primitive.ByteSize)
		v1.Read(v1Buf)
		resonator.Write(v1Buf)

		v2 := primitive.NewValue()
		v2.Set(3)
		v2.Set(7)

		v2Buf := make([]byte, primitive.ByteSize)
		v2.Read(v2Buf)

		resonator.Read(buf)
		resonator.Write(v2Buf)

		Convey("It should compose all motors in trace order", func() {
			tool := resonator.Reify()

			s1, t1 := v1.Motor()
			s2, t2 := v2.Motor()
			expectedScale, expectedTranslate := primitive.ComposeMotor(s1, t1, s2, t2)

			So(tool.Has(int(expectedScale)), ShouldBeTrue)
			So(tool.Has(int(expectedTranslate)), ShouldBeTrue)
		})
	})
}

func TestResonatorResonatorError(t *testing.T) {
	Convey("Given a ResonatorError", t, func() {
		Convey("It should implement the error interface", func() {
			var err error = ResonatorOrbitError

			So(err.Error(), ShouldEqual, "resonator: motor entered orbit, trajectory exhausted")
		})
	})
}

func BenchmarkResonatorWriteResidual(b *testing.B) {
	prompt := primitive.NewValue()

	for i := range 50 {
		prompt.Set(i * 17 % primitive.CoreBits)
	}

	output := primitive.NewValue()

	for i := range 50 {
		output.Set(i * 31 % primitive.CoreBits)
	}

	outputBuf := make([]byte, primitive.ByteSize)
	output.Read(outputBuf)

	buf := make([]byte, primitive.ByteSize)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		resonator := NewResonator(prompt)
		resonator.Read(buf)
		resonator.Write(outputBuf)
	}
}

func BenchmarkResonatorReify(b *testing.B) {
	prompt := primitive.NewValue()
	prompt.Set(0)

	buf := make([]byte, primitive.ByteSize)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		resonator := NewResonator(prompt)
		resonator.Read(buf)

		for step := range 10 {
			v := primitive.NewValue()
			v.Set(step*7 + 1)
			v.Set(step*13 + 3)

			vBuf := make([]byte, primitive.ByteSize)
			v.Read(vBuf)

			resonator.Read(buf)
			resonator.Write(vBuf)
		}

		resonator.Reify()
	}
}

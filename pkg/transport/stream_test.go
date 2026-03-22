package transport

import (
	"bytes"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

func TestRead(t *testing.T) {
	Convey("Given a Stream", t, func() {
		stream := NewStream()
		out := bytes.NewBuffer(make([]byte, 0, 1024))

		Convey("It should read from the buffer", func() {
			chunk := []byte("test")
			_, err := io.Copy(stream, bytes.NewReader(chunk))
			So(err, ShouldBeNil)
			_, err = io.Copy(out, stream)
			So(err, ShouldBeNil)
			So(out.Bytes(), ShouldEqual, chunk)
		})
	})
}

func TestReadFromEmptyBuffer(t *testing.T) {
	Convey("Given a Stream with nothing written", t, func() {
		stream := NewStream()
		buf := make([]byte, 128)
		n, err := stream.Read(buf)

		Convey("It should return 0 and EOF", func() {
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})
}

func TestWriteEmptySlice(t *testing.T) {
	Convey("Given a Stream", t, func() {
		stream := NewStream()
		n, err := stream.Write([]byte{})

		Convey("Writing empty bytes should return 0, nil", func() {
			So(n, ShouldEqual, 0)
			So(err, ShouldBeNil)
		})
	})
}

func TestWriteInstructionTriggersOperation(t *testing.T) {
	Convey("Given a Stream receiving a frame with the instruction byte set", t, func() {
		stream := NewStream()

		instrFrame := make([]byte, primitive.ByteSize)
		instrFrame[primitive.ByteSize-1] = InstructionByteMask

		n, err := stream.Write(instrFrame)

		Convey("The write should succeed and activate an operation", func() {
			So(err, ShouldBeNil)
			So(n, ShouldEqual, primitive.ByteSize)
			So(stream.operation, ShouldNotBeNil)
		})
	})
}

func TestReadWithPartiallyFilledOperation(t *testing.T) {
	Convey("Given a Stream with an active operation that has only the instruction frame", t, func() {
		stream := NewStream()

		instrFrame := make([]byte, primitive.ByteSize)
		instrFrame[primitive.ByteSize-1] = InstructionByteMask
		stream.Write(instrFrame)

		buf := make([]byte, primitive.ByteSize)
		n, err := stream.Read(buf)

		Convey("Read should return 0, nil because the ring is partially filled", func() {
			So(n, ShouldEqual, 0)
			So(err, ShouldBeNil)
			So(stream.operation, ShouldNotBeNil)
		})
	})
}

func TestReadWithCompleteOperation(t *testing.T) {
	Convey("Given a Stream with instruction + 2 operand frames written", t, func() {
		stream := NewStream()

		instrValue := primitive.NewValue()
		instrValue.SetInstruction(primitive.InstrOR)

		instrFrame := make([]byte, primitive.ByteSize)
		instrValue.Read(instrFrame)

		opA := primitive.NewValue()
		opA.Set(5)
		opA.Set(11)
		frameA := make([]byte, primitive.ByteSize)
		opA.Read(frameA)

		opB := primitive.NewValue()
		opB.Set(11)
		opB.Set(20)
		frameB := make([]byte, primitive.ByteSize)
		opB.Read(frameB)

		stream.Write(instrFrame)
		stream.Write(frameA)
		stream.Write(frameB)

		buf := make([]byte, primitive.ByteSize)
		n, err := stream.Read(buf)

		Convey("Read should return the computed OR result", func() {
			So(err, ShouldBeNil)
			So(n, ShouldEqual, primitive.ByteSize)

			result := primitive.NewValue()
			result.Write(buf)

			So(result.Has(5), ShouldBeTrue)
			So(result.Has(11), ShouldBeTrue)
			So(result.Has(20), ShouldBeTrue)
			So(result.PopCount(), ShouldEqual, 3)
		})

		Convey("The operation should be closed and cleared after read", func() {
			So(stream.operation, ShouldBeNil)
		})
	})
}

func TestReadOperationNonEOFError(t *testing.T) {
	Convey("Given a Stream with a complete operation", t, func() {
		stream := NewStream()

		instrFrame := make([]byte, primitive.ByteSize)
		instrFrame[primitive.ByteSize-1] = InstructionByteMask
		stream.Write(instrFrame)

		frameA := make([]byte, primitive.ByteSize)
		stream.Write(frameA)

		frameB := make([]byte, primitive.ByteSize)
		stream.Write(frameB)

		shortBuf := make([]byte, primitive.ByteSize-1)
		n, err := stream.Read(shortBuf)

		Convey("Read with a short buffer should propagate the non-EOF error", func() {
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, primitive.ErrShortValue)
		})
	})
}

func TestReadOperationEOFEmptyRing(t *testing.T) {
	Convey("Given a Stream with an operation whose ring has been drained", t, func() {
		stream := NewStream()

		instrFrame := make([]byte, primitive.ByteSize)
		instrFrame[primitive.ByteSize-1] = InstructionByteMask
		stream.Write(instrFrame)

		frameA := make([]byte, primitive.ByteSize)
		stream.Write(frameA)

		frameB := make([]byte, primitive.ByteSize)
		stream.Write(frameB)

		buf := make([]byte, primitive.ByteSize)
		stream.Read(buf)

		Convey("After a successful read the operation is cleared", func() {
			So(stream.operation, ShouldBeNil)
		})

		Convey("Subsequent read falls through to empty buffer path", func() {
			n, err := stream.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})
}

func TestReadOperationReturnsEOFOnEmptyRing(t *testing.T) {
	Convey("Given a Stream with an operation whose ring is empty", t, func() {
		stream := NewStream()
		stream.operation = operation.NewBitwise(operation.OR)

		buf := make([]byte, primitive.ByteSize)
		n, err := stream.Read(buf)

		Convey("Read should return 0, EOF and clear the operation", func() {
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
			So(stream.operation, ShouldBeNil)
		})
	})
}

func TestReadPipeReturnsZeroBytes(t *testing.T) {
	Convey("Given a Stream where the pipe read returns 0 bytes", t, func() {
		stream := NewStream()

		stream.pw.Write([]byte{})

		Convey("Read should return 0, EOF when the buffer is effectively empty", func() {
			buf := make([]byte, 128)
			n, err := stream.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldEqual, io.EOF)
		})
	})
}

func TestWriteToActiveOperationBypassesRingBuffer(t *testing.T) {
	Convey("Given a Stream with an active operation", t, func() {
		stream := NewStream()

		instrFrame := make([]byte, primitive.ByteSize)
		instrFrame[primitive.ByteSize-1] = InstructionByteMask
		stream.Write(instrFrame)

		So(stream.buffer.Length(), ShouldEqual, 0)

		operandFrame := make([]byte, primitive.ByteSize)
		n, err := stream.Write(operandFrame)

		Convey("Subsequent writes go to the operation, not the ring buffer", func() {
			So(err, ShouldBeNil)
			So(n, ShouldEqual, primitive.ByteSize)
			So(stream.buffer.Length(), ShouldEqual, 0)
		})
	})
}

func TestCloseWithActiveOperation(t *testing.T) {
	Convey("Given a Stream with an active operation", t, func() {
		stream := NewStream()

		instrFrame := make([]byte, primitive.ByteSize)
		instrFrame[primitive.ByteSize-1] = InstructionByteMask
		stream.Write(instrFrame)

		So(stream.operation, ShouldNotBeNil)

		err := stream.Close()

		Convey("Close should not error", func() {
			So(err, ShouldBeNil)
		})
	})
}

func TestCloseWithoutOperation(t *testing.T) {
	Convey("Given a Stream with no active operation", t, func() {
		stream := NewStream()

		data := []byte("hello")
		stream.Write(data)

		err := stream.Close()

		Convey("Close should not error", func() {
			So(err, ShouldBeNil)
		})
	})
}

func TestWithBufferSize(t *testing.T) {
	Convey("Given WithBufferSize option", t, func() {
		stream := NewStream(WithBufferSize(8192))

		Convey("The buffer size should be stored", func() {
			So(stream, ShouldNotBeNil)
			So(stream.bufferSize, ShouldEqual, 8192)
		})
	})
}

func TestStreamError(t *testing.T) {
	Convey("Given StreamError types", t, func() {
		Convey("NewStreamError should create an error with the correct message", func() {
			err := NewStreamError(StreamErrorTypeBufferNil)
			So(err, ShouldNotBeNil)
			So(err.Message, ShouldEqual, string(StreamErrorTypeBufferNil))
		})

		Convey("Error() should format the message and wrapped error", func() {
			err := NewStreamError(StreamErrorTypeBufferRead)
			So(err.Error(), ShouldContainSubstring, "stream error")
			So(err.Error(), ShouldContainSubstring, "buffer read error")
		})

		Convey("All error types should be distinct", func() {
			types := []StreamErrorType{
				StreamErrorTypeBufferNil,
				StreamErrorTypeBufferEmpty,
				StreamErrorTypeBufferFull,
				StreamErrorTypeBufferRead,
				StreamErrorTypeBufferWrite,
			}

			seen := make(map[StreamErrorType]bool)

			for _, t := range types {
				So(seen[t], ShouldBeFalse)
				seen[t] = true
			}
		})
	})
}

func TestReadWriteDataRoundTrip(t *testing.T) {
	Convey("Given a Stream with multiple data writes", t, func() {
		stream := NewStream()

		chunk1 := []byte("hello")
		chunk2 := []byte("world")

		_, err1 := stream.Write(chunk1)
		So(err1, ShouldBeNil)

		_, err2 := stream.Write(chunk2)
		So(err2, ShouldBeNil)

		Convey("Read should return both chunks in order", func() {
			buf := make([]byte, 128)
			n, err := stream.Read(buf)
			So(err, ShouldBeNil)
			So(n, ShouldBeGreaterThan, 0)
			So(string(buf[:n]), ShouldEqual, "helloworld")
		})
	})
}

func TestInstructionByteDetection(t *testing.T) {
	Convey("Given different last-byte values", t, func() {
		Convey("Frame without instruction bit should go to the ring buffer", func() {
			stream := NewStream()
			frame := make([]byte, primitive.ByteSize)
			frame[primitive.ByteSize-1] = 0x7F

			stream.Write(frame)

			So(stream.operation, ShouldBeNil)
			So(stream.buffer.Length(), ShouldBeGreaterThan, 0)
		})

		Convey("Frame with instruction bit should activate an operation", func() {
			stream := NewStream()
			frame := make([]byte, primitive.ByteSize)
			frame[primitive.ByteSize-1] = 0x80

			stream.Write(frame)

			So(stream.operation, ShouldNotBeNil)
			So(stream.buffer.Length(), ShouldEqual, 0)
		})

		Convey("Frame with instruction bit plus data should activate an operation", func() {
			stream := NewStream()
			frame := make([]byte, primitive.ByteSize)
			frame[primitive.ByteSize-1] = 0xFF

			stream.Write(frame)

			So(stream.operation, ShouldNotBeNil)
		})
	})
}

func BenchmarkStreamWriteRead(b *testing.B) {
	stream := NewStream()
	data := make([]byte, 256)

	for i := range data {
		data[i] = byte(i)
	}

	buf := make([]byte, 256)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		stream.Write(data)
		stream.Read(buf)
	}
}

func BenchmarkStreamOperationCycle(b *testing.B) {
	instrFrame := make([]byte, primitive.ByteSize)
	instrFrame[primitive.ByteSize-1] = InstructionByteMask

	frameA := make([]byte, primitive.ByteSize)
	frameA[0] = 0x05

	frameB := make([]byte, primitive.ByteSize)
	frameB[0] = 0x0A

	buf := make([]byte, primitive.ByteSize)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		stream := NewStream()
		stream.Write(instrFrame)
		stream.Write(frameA)
		stream.Write(frameB)
		stream.Read(buf)
		stream.Close()
	}
}

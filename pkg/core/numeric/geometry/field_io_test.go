package geometry

import (
	"io"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func frameBytes(value *primitive.Value) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), valueFrameSize)
}

func TestFieldWrite(t *testing.T) {
	Convey("Given a community field", t, func() {
		field := NewCommunityField(Mod257)

		Convey("Write should accept a full Value frame and return valueFrameSize", func() {
			var value primitive.Value
			value[0] = 0xDEADBEEF

			n, err := field.Write(frameBytes(&value))

			So(err, ShouldBeNil)
			So(n, ShouldEqual, valueFrameSize)
		})

		Convey("Write should return ErrShortBuffer when p is smaller than a Value frame", func() {
			n, err := field.Write([]byte{0x01})

			So(err, ShouldEqual, io.ErrShortBuffer)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestFieldRead(t *testing.T) {
	Convey("Given a community field with no emitted Values", t, func() {
		field := NewCommunityField(Mod257)

		Convey("Read should return io.EOF when the output ring is empty", func() {
			buf := make([]byte, valueFrameSize)
			n, err := field.Read(buf)

			So(err, ShouldEqual, io.EOF)
			So(n, ShouldEqual, 0)
		})

		Convey("Read should return the bytes pushed by EmitValue", func() {
			var emitted primitive.Value
			emitted[5] = 0xCAFEBABE

			field.EmitValue(&emitted)

			buf := make([]byte, valueFrameSize)
			n, err := field.Read(buf)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, valueFrameSize)

			var recovered primitive.Value
			copy(
				unsafe.Slice((*byte)(unsafe.Pointer(&recovered[0])), valueFrameSize),
				buf[:valueFrameSize],
			)

			So(recovered[5], ShouldEqual, emitted[5])
		})

		Convey("Read should return ErrShortBuffer when p is too small", func() {
			n, err := field.Read([]byte{0x00})

			So(err, ShouldEqual, io.ErrShortBuffer)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestFieldClose(t *testing.T) {
	Convey("Given a community field that has been used for I/O", t, func() {
		field := NewCommunityField(Mod257)
		var value primitive.Value
		field.Write(frameBytes(&value)) //nolint:errcheck

		Convey("Close should not return an error", func() {
			So(field.Close(), ShouldBeNil)
		})
	})

	Convey("Given a community field that has never performed I/O", t, func() {
		field := NewCommunityField(Mod257)

		Convey("Close should not panic and should return nil", func() {
			So(field.Close(), ShouldBeNil)
		})
	})
}

func TestFieldDrainIntake(t *testing.T) {
	Convey("Given a community field with multiple Values written to its intake", t, func() {
		field := NewCommunityField(Mod257)

		var a, b, c primitive.Value
		a[0] = 1
		b[0] = 2
		c[0] = 3

		field.Write(frameBytes(&a)) //nolint:errcheck
		field.Write(frameBytes(&b)) //nolint:errcheck
		field.Write(frameBytes(&c)) //nolint:errcheck

		Convey("DrainIntake should move all pending Values into field.Values", func() {
			before := len(field.Values)
			field.DrainIntake()

			So(len(field.Values), ShouldEqual, before+3)
		})

		Convey("DrainIntake called again on an empty ring should be a no-op", func() {
			field.DrainIntake()
			countAfterFirst := len(field.Values)

			field.DrainIntake()

			So(len(field.Values), ShouldEqual, countAfterFirst)
		})
	})

	Convey("Given a community field whose intake ring has never been initialised", t, func() {
		field := NewCommunityField(Mod257)

		Convey("DrainIntake should not panic", func() {
			So(func() { field.DrainIntake() }, ShouldNotPanic)
		})
	})
}

func TestFieldEmitValue(t *testing.T) {
	Convey("Given a community field and a Value to emit", t, func() {
		field := NewCommunityField(Mod257)

		var emitted primitive.Value
		emitted[10] = 0xABCD

		field.EmitValue(&emitted)

		Convey("EmitValue should make the Value available via Read", func() {
			buf := make([]byte, valueFrameSize)
			n, err := field.Read(buf)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, valueFrameSize)

			var recovered primitive.Value
			copy(
				unsafe.Slice((*byte)(unsafe.Pointer(&recovered[0])), valueFrameSize),
				buf[:valueFrameSize],
			)

			So(recovered[10], ShouldEqual, emitted[10])
		})
	})

	Convey("Given a community field with a connected peer", t, func() {
		source := NewCommunityField(Mod257)
		sink := NewCommunityField(Mod257)
		source.Connect(sink)

		var emitted primitive.Value
		emitted[7] = 0x1234

		source.EmitValue(&emitted)

		Convey("EmitValue should fan out to the connected peer", func() {
			sink.DrainIntake()

			So(len(sink.Values), ShouldEqual, 1)
			So((*sink.Values[0])[7], ShouldEqual, emitted[7])
		})
	})
}

func TestFieldConnect(t *testing.T) {
	Convey("Given a community field", t, func() {
		field := NewCommunityField(Mod257)

		Convey("Connect should add a peer to the fan-out list", func() {
			peer := NewCommunityField(Mod257)

			So(len(field.peers), ShouldEqual, 0)
			field.Connect(peer)
			So(len(field.peers), ShouldEqual, 1)
		})
	})
}

func TestFieldDisconnect(t *testing.T) {
	Convey("Given a community field with two connected peers", t, func() {
		field := NewCommunityField(Mod257)
		peerA := NewCommunityField(Mod257)
		peerB := NewCommunityField(Mod257)

		field.Connect(peerA)
		field.Connect(peerB)

		Convey("Disconnect should remove only the target peer", func() {
			field.Disconnect(peerA)

			So(len(field.peers), ShouldEqual, 1)
			So(field.peers[0], ShouldEqual, peerB)
		})

		Convey("Disconnect with an unknown peer should leave the list unchanged", func() {
			stranger := NewCommunityField(Mod257)
			field.Disconnect(stranger)

			So(len(field.peers), ShouldEqual, 2)
		})
	})
}

func BenchmarkFieldWrite(b *testing.B) {
	field := NewCommunityField(Mod257)
	var value primitive.Value
	buf := frameBytes(&value)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		field.Write(buf) //nolint:errcheck
		field.intake.Pop()
	}
}

func BenchmarkFieldRead(b *testing.B) {
	field := NewCommunityField(Mod257)
	var value primitive.Value
	buf := make([]byte, valueFrameSize)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		field.EmitValue(&value)
		field.Read(buf) //nolint:errcheck
	}
}

func BenchmarkFieldEmitValueWithPeer(b *testing.B) {
	source := NewCommunityField(Mod257)
	sink := NewCommunityField(Mod257)
	source.Connect(sink)

	var value primitive.Value

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		source.EmitValue(&value)
		source.output.Pop()
		sink.intake.Pop()
	}
}

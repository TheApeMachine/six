package gossip

import (
	"context"
	"io"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func makeFrame(seed uint64) []byte {
	value := new(primitive.Value)
	(*value)[0] = seed
	return unsafe.Slice((*byte)(unsafe.Pointer(&(*value)[0])), connFrameSize)
}

func TestNewConn(t *testing.T) {
	Convey("Given a valid context", t, func() {
		conn := NewConn(context.Background())

		Convey("NewConn should return a non-nil Conn with an initialised intake ring", func() {
			So(conn, ShouldNotBeNil)
			So(conn.intake, ShouldNotBeNil)
		})
	})
}

func TestConnWrite(t *testing.T) {
	Convey("Given a Conn", t, func() {
		conn := NewConn(context.Background())

		Convey("Write should accept a full Value frame and return connFrameSize", func() {
			n, err := conn.Write(makeFrame(0xABCD))

			So(err, ShouldBeNil)
			So(n, ShouldEqual, connFrameSize)
		})

		Convey("Write should return ErrShortBuffer for frames smaller than connFrameSize", func() {
			n, err := conn.Write([]byte{0x01, 0x02})

			So(err, ShouldEqual, io.ErrShortBuffer)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestConnRead(t *testing.T) {
	Convey("Given a Conn with no written frames", t, func() {
		conn := NewConn(context.Background())

		Convey("Read should return io.EOF when the intake ring is empty", func() {
			buf := make([]byte, connFrameSize)
			n, err := conn.Read(buf)

			So(err, ShouldEqual, io.EOF)
			So(n, ShouldEqual, 0)
		})

		Convey("Read should return the frame that was most recently written", func() {
			const sentinel uint64 = 0xDEADC0DE

			conn.Write(makeFrame(sentinel)) //nolint:errcheck

			buf := make([]byte, connFrameSize)
			n, err := conn.Read(buf)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, connFrameSize)

			var recovered primitive.Value
			copy(
				unsafe.Slice((*byte)(unsafe.Pointer(&recovered[0])), connFrameSize),
				buf[:connFrameSize],
			)

			So(recovered[0], ShouldEqual, sentinel)
		})

		Convey("Read should return ErrShortBuffer when p is smaller than connFrameSize", func() {
			n, err := conn.Read([]byte{0x00})

			So(err, ShouldEqual, io.ErrShortBuffer)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestConnClose(t *testing.T) {
	Convey("Given an active Conn", t, func() {
		conn := NewConn(context.Background())

		Convey("Close should return nil and cancel the intake ring's context", func() {
			So(conn.Close(), ShouldBeNil)
			So(conn.intake.Error(), ShouldBeNil)
		})
	})

	Convey("Given a Conn with a nil intake ring", t, func() {
		conn := &Conn{}

		Convey("Close should not panic and should return nil", func() {
			So(conn.Close(), ShouldBeNil)
		})
	})
}

func TestConnSetAffinity(t *testing.T) {
	Convey("Given a Conn", t, func() {
		conn := NewConn(context.Background())

		Convey("SetAffinity then Affinity should round-trip the 5 words", func() {
			target := []uint64{1, 2, 3, 4, 5}
			conn.SetAffinity(target)

			So(conn.Affinity(), ShouldResemble, target)
		})
	})
}

func TestConnAddPeer(t *testing.T) {
	Convey("Given a Conn", t, func() {
		conn := NewConn(context.Background())

		Convey("AddPeer should register the peer in the PriorityRoute", func() {
			peer := NewConn(context.Background())
			conn.AddPeer(peer, []uint64{0xFF, 0, 0, 0, 0})

			So(len(conn.route), ShouldEqual, 1)
			So(conn.route[0].Dst(), ShouldEqual, peer)
		})
	})
}

func TestConnBroadcast(t *testing.T) {
	Convey("Given a Conn with two registered peers", t, func() {
		conn := NewConn(context.Background())
		peerA := NewConn(context.Background())
		peerB := NewConn(context.Background())

		conn.AddPeer(peerA, []uint64{})
		conn.AddPeer(peerB, []uint64{})

		frame := makeFrame(0x1234)

		Convey("Broadcast should write the frame to all peers", func() {
			n, err := conn.Broadcast(frame)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, connFrameSize)

			bufA := make([]byte, connFrameSize)
			bufB := make([]byte, connFrameSize)

			nA, errA := peerA.Read(bufA)
			nB, errB := peerB.Read(bufB)

			So(errA, ShouldBeNil)
			So(errB, ShouldBeNil)
			So(nA, ShouldEqual, connFrameSize)
			So(nB, ShouldEqual, connFrameSize)
		})
	})
}

func TestConnReceive(t *testing.T) {
	Convey("Given a Conn and a primitive.Value", t, func() {
		conn := NewConn(context.Background())

		var value primitive.Value
		value[3] = 0xFEEDFACE

		Convey("Receive should push the Value into the intake ring for subsequent Read", func() {
			conn.Receive(&value)

			buf := make([]byte, connFrameSize)
			n, err := conn.Read(buf)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, connFrameSize)

			var recovered primitive.Value
			copy(
				unsafe.Slice((*byte)(unsafe.Pointer(&recovered[0])), connFrameSize),
				buf[:connFrameSize],
			)

			So(recovered[3], ShouldEqual, value[3])
		})

		Convey("Receive with a nil Value should not panic", func() {
			So(func() { conn.Receive(nil) }, ShouldNotPanic)
		})
	})
}

func BenchmarkConnWrite(b *testing.B) {
	conn := NewConn(context.Background())
	frame := makeFrame(1)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		conn.Write(frame) //nolint:errcheck
		conn.intake.Pop()
	}
}

func BenchmarkConnRead(b *testing.B) {
	conn := NewConn(context.Background())
	frame := makeFrame(1)
	buf := make([]byte, connFrameSize)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		conn.Write(frame) //nolint:errcheck
		conn.Read(buf)    //nolint:errcheck
	}
}

func BenchmarkConnBroadcast(b *testing.B) {
	conn := NewConn(context.Background())
	peers := make([]*Conn, 4)

	for idx := range peers {
		peers[idx] = NewConn(context.Background())
		conn.AddPeer(peers[idx], []uint64{})
	}

	frame := makeFrame(42)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		conn.Broadcast(frame) //nolint:errcheck

		for _, peer := range peers {
			peer.intake.Pop()
		}
	}
}

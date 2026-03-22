package network

import (
	"io"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestUniConn(t *testing.T) {
	gc.Convey("Given a UniConn with no transport", t, func() {
		conn := NewUniConn()

		gc.Convey("It should return ErrNoTransport on Read", func() {
			buf := make([]byte, 1024)
			_, err := conn.Read(buf)
			gc.So(err, gc.ShouldEqual, ErrNoTransport)
		})

		gc.Convey("It should return ErrNoTransport on Write", func() {
			_, err := conn.Write([]byte("hello"))
			gc.So(err, gc.ShouldEqual, ErrNoTransport)
		})

		gc.Convey("It should close without error", func() {
			gc.So(conn.Close(), gc.ShouldBeNil)
		})
	})

	gc.Convey("Given a UniConn backed by an IPC transport", t, func() {
		path := t.TempDir() + "/uniconn_ipc.sock"
		server := NewIPC(IPCWithListen(path))

		done := make(chan struct{})
		go func() {
			defer close(done)
			server.Accept()
		}()

		client := NewIPC(IPCWithDial(path))
		<-done

		conn := NewUniConn(UniConnWithIPC(client))

		gc.Convey("It should delegate Write through to the IPC socket", func() {
			payload := make([]byte, 1024)
			payload[0] = 0xAB

			n, err := conn.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1024)
			n, err = server.Read(buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf[0], gc.ShouldEqual, 0xAB)
		})

		gc.Reset(func() {
			conn.Close()
			server.Close()
		})
	})
}

func TestUniConnImplementsRWC(t *testing.T) {
	gc.Convey("Given a UniConn", t, func() {
		conn := NewUniConn()

		gc.Convey("It should satisfy io.ReadWriteCloser", func() {
			var _ io.ReadWriteCloser = conn
			gc.So(conn, gc.ShouldNotBeNil)
		})
	})
}

func BenchmarkUniConnWrite(b *testing.B) {
	path := b.TempDir() + "/bench_uniconn.sock"
	server := NewIPC(IPCWithListen(path))

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Accept()
	}()

	client := NewIPC(IPCWithDial(path))
	<-done

	conn := NewUniConn(UniConnWithIPC(client))
	payload := make([]byte, 1024)
	sink := make([]byte, 1024)

	go func() {
		for {
			_, err := server.Read(sink)
			if err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(1024)

	for b.Loop() {
		conn.Write(payload)
	}

	b.StopTimer()
	conn.Close()
	server.Close()
}

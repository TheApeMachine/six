package network

import (
	"context"
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

func TestUniConnWithUDP(t *testing.T) {
	gc.Convey("Given a UniConn backed by a UDP multicast transport", t, func() {
		group := "224.0.0.251:9997"
		listener := NewUDPMulticast(UDPMulticastWithListener(group, ""))
		gc.So(listener.err, gc.ShouldBeNil)

		sender := NewUDPMulticast(UDPMulticastWithDialer(group))
		gc.So(sender.err, gc.ShouldBeNil)

		conn := NewUniConn(UniConnWithUDP(sender))

		gc.Convey("It should set the connection type to UDPType", func() {
			gc.So(conn.connType, gc.ShouldEqual, UDPType)
		})

		gc.Convey("It should delegate Write to the UDP transport", func() {
			payload := make([]byte, 1024)
			payload[0] = 0xCD

			n, err := conn.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1500)
			n, err = listener.Read(buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf[0], gc.ShouldEqual, 0xCD)
		})

		gc.Reset(func() {
			conn.Close()
			listener.Close()
		})
	})
}

func TestUniConnReadDelegation(t *testing.T) {
	gc.Convey("Given a UniConn backed by IPC for bidirectional transfer", t, func() {
		path := t.TempDir() + "/r.sock"
		server := NewIPC(IPCWithListen(path))
		gc.So(server.err, gc.ShouldBeNil)

		accepted := make(chan error, 1)
		go func() {
			accepted <- server.Accept()
		}()

		client := NewIPC(IPCWithDial(path))
		gc.So(client.err, gc.ShouldBeNil)
		gc.So(<-accepted, gc.ShouldBeNil)

		conn := NewUniConn(UniConnWithIPC(client))

		gc.Convey("It should delegate Read from the underlying transport", func() {
			payload := make([]byte, 1024)
			payload[0] = 0xEF

			n, err := server.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1024)
			n, err = io.ReadFull(conn, buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf[0], gc.ShouldEqual, 0xEF)
		})

		gc.Reset(func() {
			conn.Close()
			server.Close()
		})
	})
}

func TestUniConnWithContext(t *testing.T) {
	gc.Convey("Given a UniConn with a custom context", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		conn := NewUniConn(UniConnWithContext(ctx))

		gc.Convey("It should propagate cancellation from the parent context", func() {
			gc.So(conn.ctx.Err(), gc.ShouldBeNil)
			cancel()
			gc.So(conn.ctx.Err(), gc.ShouldNotBeNil)
		})

		gc.Reset(func() {
			cancel()
			conn.Close()
		})
	})
}

func TestUniConnConnType(t *testing.T) {
	gc.Convey("Given UniConn transport option functions", t, func() {
		gc.Convey("UniConnWithUDP should set UDPType", func() {
			udp := NewUDPMulticast()
			conn := NewUniConn(UniConnWithUDP(udp))
			gc.So(conn.connType, gc.ShouldEqual, UDPType)
			conn.Close()
		})

		gc.Convey("UniConnWithQUIC should set QUICType", func() {
			q := NewQUIC()
			conn := NewUniConn(UniConnWithQUIC(q))
			gc.So(conn.connType, gc.ShouldEqual, QUICType)
			conn.Close()
		})
	})
}

func TestUniConnErrorString(t *testing.T) {
	gc.Convey("Given UniConnError constants", t, func() {
		gc.Convey("ErrNoTransport should satisfy the error interface", func() {
			var err error = ErrNoTransport
			gc.So(err.Error(), gc.ShouldEqual, "uniconn: no transport configured")
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

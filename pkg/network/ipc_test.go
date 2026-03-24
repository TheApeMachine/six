package network

import (
	"io"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestIPC(t *testing.T) {
	gc.Convey("Given an IPC transport with no connection", t, func() {
		ipc := NewIPC()

		gc.Convey("It should return ErrIPCNotConnected on Read", func() {
			buf := make([]byte, 1024)
			_, err := ipc.Read(buf)
			gc.So(err, gc.ShouldEqual, ErrIPCNotConnected)
		})

		gc.Convey("It should return ErrIPCNotConnected on Write", func() {
			_, err := ipc.Write([]byte("data"))
			gc.So(err, gc.ShouldEqual, ErrIPCNotConnected)
		})

		gc.Convey("It should return ErrIPCNotListening on Accept", func() {
			gc.So(ipc.Accept(), gc.ShouldEqual, ErrIPCNotListening)
		})
	})

	gc.Convey("Given a listening IPC server and a dialing client", t, func() {
		path := t.TempDir() + "/ipc_test.sock"
		server := NewIPC(IPCWithListen(path))
		gc.So(server.err, gc.ShouldBeNil)

		accepted := make(chan error, 1)
		go func() {
			accepted <- server.Accept()
		}()

		client := NewIPC(IPCWithDial(path))
		gc.So(client.err, gc.ShouldBeNil)
		gc.So(<-accepted, gc.ShouldBeNil)

		gc.Convey("It should transfer a full 1024-byte Value from client to server", func() {
			payload := make([]byte, 1024)
			for i := range payload {
				payload[i] = byte(i % 256)
			}

			n, err := client.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1024)
			n, err = io.ReadFull(server, buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf, gc.ShouldResemble, payload)
		})

		gc.Convey("It should transfer a full 1024-byte Value from server to client", func() {
			payload := make([]byte, 1024)
			payload[0] = 0xDE
			payload[1023] = 0xAD

			n, err := server.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1024)
			n, err = io.ReadFull(client, buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf[0], gc.ShouldEqual, 0xDE)
			gc.So(buf[1023], gc.ShouldEqual, 0xAD)
		})

		gc.Reset(func() {
			client.Close()
			server.Close()
		})
	})
}

func TestIPCImplementsRWC(t *testing.T) {
	gc.Convey("Given an IPC transport", t, func() {
		ipc := NewIPC()

		gc.Convey("It should satisfy io.ReadWriteCloser", func() {
			var _ io.ReadWriteCloser = ipc
			gc.So(ipc, gc.ShouldNotBeNil)
		})
	})
}

func BenchmarkIPCRoundtrip(b *testing.B) {
	path := b.TempDir() + "/bench_ipc.sock"
	server := NewIPC(IPCWithListen(path))

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Accept()
	}()

	client := NewIPC(IPCWithDial(path))
	<-done

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
		client.Write(payload)
	}

	b.StopTimer()
	client.Close()
	server.Close()
}

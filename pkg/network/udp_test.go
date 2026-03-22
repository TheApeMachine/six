package network

import (
	"io"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestUDPMulticast(t *testing.T) {
	gc.Convey("Given a UDPMulticast with no socket", t, func() {
		udp := NewUDPMulticast()

		gc.Convey("It should return ErrUDPNotBound on Read", func() {
			buf := make([]byte, 1024)
			_, err := udp.Read(buf)
			gc.So(err, gc.ShouldEqual, ErrUDPNotBound)
		})

		gc.Convey("It should return ErrUDPNotBound on Write", func() {
			_, err := udp.Write([]byte("data"))
			gc.So(err, gc.ShouldEqual, ErrUDPNotBound)
		})

		gc.Convey("It should close without error", func() {
			gc.So(udp.Close(), gc.ShouldBeNil)
		})
	})

	gc.Convey("Given a UDP multicast listener and dialer on the same group", t, func() {
		group := "224.0.0.251:9999"

		listener := NewUDPMulticast(UDPMulticastWithListener(group, ""))
		gc.So(listener.err, gc.ShouldBeNil)

		sender := NewUDPMulticast(UDPMulticastWithDialer(group))
		gc.So(sender.err, gc.ShouldBeNil)

		gc.Convey("It should deliver a datagram from sender to listener", func() {
			payload := make([]byte, 1024)
			payload[0] = 0x42
			payload[1023] = 0xFF

			n, err := sender.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1500)
			n, err = listener.Read(buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf[0], gc.ShouldEqual, 0x42)
			gc.So(buf[1023], gc.ShouldEqual, 0xFF)
		})

		gc.Reset(func() {
			sender.Close()
			listener.Close()
		})
	})
}

func TestUDPMulticastImplementsRWC(t *testing.T) {
	gc.Convey("Given a UDPMulticast", t, func() {
		udp := NewUDPMulticast()

		gc.Convey("It should satisfy io.ReadWriteCloser", func() {
			var _ io.ReadWriteCloser = udp
			gc.So(udp, gc.ShouldNotBeNil)
		})
	})
}

func BenchmarkUDPMulticast(b *testing.B) {
	group := "224.0.0.251:9998"

	listener := NewUDPMulticast(UDPMulticastWithListener(group, ""))
	sender := NewUDPMulticast(UDPMulticastWithDialer(group))

	payload := make([]byte, 1024)
	sink := make([]byte, 1500)

	go func() {
		for {
			_, err := listener.Read(sink)
			if err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(1024)

	for b.Loop() {
		sender.Write(payload)
	}

	b.StopTimer()
	sender.Close()
	listener.Close()
}

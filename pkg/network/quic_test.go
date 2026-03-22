package network

import (
	"io"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestQUIC(t *testing.T) {
	gc.Convey("Given a QUIC transport with no stream", t, func() {
		q := NewQUIC()

		gc.Convey("It should return ErrQUICNoStream on Read", func() {
			buf := make([]byte, 1024)
			_, err := q.Read(buf)
			gc.So(err, gc.ShouldEqual, ErrQUICNoStream)
		})

		gc.Convey("It should return ErrQUICNoStream on Write", func() {
			_, err := q.Write([]byte("data"))
			gc.So(err, gc.ShouldEqual, ErrQUICNoStream)
		})

		gc.Convey("It should return ErrQUICNotListening on Accept", func() {
			gc.So(q.Accept(), gc.ShouldEqual, ErrQUICNotListening)
		})

		gc.Convey("It should close without error", func() {
			gc.So(q.Close(), gc.ShouldBeNil)
		})
	})
}

func TestQUICImplementsRWC(t *testing.T) {
	gc.Convey("Given a QUIC transport", t, func() {
		q := NewQUIC()

		gc.Convey("It should satisfy io.ReadWriteCloser", func() {
			var _ io.ReadWriteCloser = q
			gc.So(q, gc.ShouldNotBeNil)
		})
	})
}

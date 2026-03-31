package network

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestRead(t *testing.T) {
	Convey("Given a UniConn with no transport", t, func() {
		conn := NewUniConn(t.Context())
		So(conn, ShouldNotBeNil)

		Convey("Ready returns an error", func() {
			err := conn.Ready()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, string(ErrTransportFailure))
		})
	})
}

func TestWrite(t *testing.T) {
	Convey("Given a UniConn with no transport", t, func() {
		conn := NewUniConn(t.Context())
		So(conn, ShouldNotBeNil)

		Convey("Ready returns an error", func() {
			err := conn.Ready()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, string(ErrTransportFailure))
		})
	})
}

func TestClose(t *testing.T) {
	Convey("Given a UniConn with no transport", t, func() {
		conn := NewUniConn(t.Context())
		So(conn, ShouldNotBeNil)

		Convey("Close returns nil", func() {
			err := conn.Close()
			So(err, ShouldBeNil)
		})
	})
}

func TestEnsureReady(t *testing.T) {
	Convey("Given a UniConn with no transport", t, func() {
		conn := NewUniConn(t.Context())
		So(conn, ShouldNotBeNil)

		Convey("ensureReady returns an error", func() {
			err := conn.ensureReady()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, string(ErrTransportFailure))
		})
	})
}

func BenchmarkRead(b *testing.B) {
	conn := NewUniConn(b.Context())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.Read(nil)
	}
}

func BenchmarkWrite(b *testing.B) {
	conn := NewUniConn(b.Context())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.Write(nil)
	}
}

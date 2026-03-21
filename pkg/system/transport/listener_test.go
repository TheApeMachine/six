package transport

import (
	"context"
	"testing"

	"capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
)

func TestListenerTaskWrite(t *testing.T) {
	Convey("listenerTask should accept writes as a no-op carrier", t, func() {
		task := &listenerTask{listener: &Listener{}}
		n, err := task.Write([]byte{1, 2, 3})
		So(n, ShouldEqual, 3)
		So(err, ShouldBeNil)
		So(task.Close(), ShouldBeNil)
	})
}

func TestListenerDialLifecycle(t *testing.T) {
	Convey("Given a TCP listener with ephemeral port", t, func() {
		ctx := context.Background()
		listener, err := NewListener(ctx, "127.0.0.1:0", capnp.Client{})
		So(err, ShouldBeNil)
		So(listener.Addr(), ShouldNotBeEmpty)

		Convey("It should open an RPC connection on Dial", func() {
			conn, dialErr := Dial(ctx, listener.Addr())
			So(dialErr, ShouldBeNil)
			So(conn, ShouldNotBeNil)
			So(conn.Close(), ShouldBeNil)
		})

		So(listener.Close(), ShouldBeNil)
	})
}

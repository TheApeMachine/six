package reader

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewReaderServer(t *testing.T) {
	Convey("Given a reader server with context", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rdr := NewReaderServer(ReaderWithContext(ctx))
		So(rdr, ShouldNotBeNil)
		So(rdr.ctx, ShouldNotBeNil)
		So(rdr.clientConns, ShouldNotBeNil)
	})
}

func TestReaderServerClient(t *testing.T) {
	Convey("Client should register an entry without panicking", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rdr := NewReaderServer(ReaderWithContext(ctx))
		readerCap := rdr.Client("peer-a")
		So(readerCap.IsValid(), ShouldBeTrue)
		So(len(rdr.clientConns), ShouldEqual, 1)
	})
}

func TestReaderServerClose(t *testing.T) {
	Convey("Close should reset reader state", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rdr := NewReaderServer(ReaderWithContext(ctx))
		So(rdr.Close(), ShouldBeNil)
	})
}

func BenchmarkReaderServerStartStop(b *testing.B) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		rdr := NewReaderServer(ReaderWithContext(ctx))
		_ = rdr.Close()
	}
}

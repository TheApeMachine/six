package transport

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPumpClose(t *testing.T) {
	Convey("Close should finish without hanging", t, func() {
		pump := NewPump(NewStream())

		finished := make(chan struct{})

		go func() {
			_ = pump.Close()
			close(finished)
		}()

		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatal("Pump.Close blocked past deadline")
		}
	})
}

func TestPumpCloseIdempotent(t *testing.T) {
	Convey("Second Close should be harmless", t, func() {
		pump := NewPump(NewStream())
		So(pump.Close(), ShouldBeNil)
		So(pump.Close(), ShouldBeNil)
	})
}

func BenchmarkPumpWrite(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		pipeline := NewStream()
		pump := NewPump(pipeline)

		if _, err := pump.Write([]byte{0}); err != nil {
			b.Fatal(err)
		}

		_ = pump.Close()
	}
}

package transport

import (
	"io"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPumpClose(t *testing.T) {
	Convey("Close should finish without hanging", t, func() {
		pump := NewPump(NewStream())

		finished := make(chan struct{})
		var closeErr error

		go func() {
			closeErr = pump.Close()
			close(finished)
		}()

		select {
		case <-finished:
			So(closeErr, ShouldBeNil)
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

func TestPumpCloseWithConcurrentPassthroughReader(t *testing.T) {
	Convey("Close should finish even with a competing passthrough reader", t, func() {
		pump := NewPump(NewStream())
		payload := []byte{0}

		go func() {
			_, _ = io.Copy(io.Discard, pump.passthrough)
		}()

		for range 1024 {
			_, writeErr := pump.Write(payload)
			So(writeErr, ShouldBeNil)
		}

		finished := make(chan struct{})
		var closeErr error

		go func() {
			closeErr = pump.Close()
			close(finished)
		}()

		select {
		case <-finished:
			So(closeErr, ShouldBeNil)
		case <-time.After(3 * time.Second):
			t.Fatal("Pump.Close blocked with concurrent passthrough reader")
		}
	})
}

func BenchmarkPumpWrite(b *testing.B) {
	pipeline := NewStream()
	pump := NewPump(pipeline)
	payload := []byte{0}
	drainErr := make(chan error, 1)

	b.ReportAllocs()

	go func() {
		_, err := io.Copy(io.Discard, pump.passthrough)
		drainErr <- err
	}()

	defer func() {
		readErr := <-drainErr
		if readErr != nil {
			b.Fatal(readErr)
		}
	}()
	defer pump.Close()

	for b.Loop() {
		if _, err := pump.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

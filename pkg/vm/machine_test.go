package vm

import (
	"bytes"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/theapemachine/six/pkg/core"
)

func TestNewMachine(t *testing.T) {
	Convey("NewMachine", t, func() {
		Convey("returns a validated machine wired to tokenizer and backend", func() {
			machine, err := NewMachine(t.Context())
			So(err, ShouldBeNil)
			So(machine, ShouldNotBeNil)
			So(machine.Close(), ShouldBeNil)
		})
	})
}

func TestMachineClose(t *testing.T) {
	Convey("Close", t, func() {
		Convey("stops the background copy loop without error", func() {
			machine, err := NewMachine(t.Context())
			So(err, ShouldBeNil)
			So(machine.Close(), ShouldBeNil)
		})
	})
}

func TestMachineRead(t *testing.T) {
	Convey("Read", t, func() {
		Convey("returns context error after Close", func() {
			machine, err := NewMachine(t.Context())
			So(err, ShouldBeNil)

			So(machine.Close(), ShouldBeNil)

			buf := make([]byte, 64)
			_, readErr := machine.Read(buf)
			So(readErr, ShouldNotBeNil)
		})
	})
}

func TestMachineWrite(t *testing.T) {
	Convey("Write", t, func() {
		Convey("accepts payloads when destinations are wired", func() {
			machine, err := NewMachine(t.Context())
			So(err, ShouldBeNil)
			defer machine.Close()

			payload := bytes.Repeat([]byte("z"), 16)
			n, writeErr := machine.Write(payload)
			So(writeErr, ShouldBeNil)
			// Write encodes through NewValue and persists the full wire frame
			// (1024 bytes), not the raw ingress byte count.
			So(n, ShouldEqual, int(core.Cfg.Value.Bytes))
		})
	})
}

func TestWithSources(t *testing.T) {
	Convey("WithSources", t, func() {
		Convey("prepends readers to the tokenizer source chain", func() {
			machine, err := NewMachine(
				t.Context(),
				WithSources(bytes.NewBufferString("")),
			)
			So(err, ShouldBeNil)
			So(machine, ShouldNotBeNil)
			So(machine.Close(), ShouldBeNil)
		})
	})
}

func TestWithDestinations(t *testing.T) {
	Convey("WithDestinations", t, func() {
		Convey("appends writers to the tokenizer destination chain", func() {
			sink := &bytes.Buffer{}

			machine, err := NewMachine(
				t.Context(),
				WithDestinations(sink),
			)
			So(err, ShouldBeNil)
			So(machine, ShouldNotBeNil)
			So(machine.Close(), ShouldBeNil)
		})
	})
}

func BenchmarkNewMachine(b *testing.B) {
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		machine, err := NewMachine(ctx)

		if err != nil {
			b.Fatal(err)
		}

		_ = machine.Close()
	}
}

func BenchmarkMachineWrite(b *testing.B) {
	machine, err := NewMachine(b.Context())

	if err != nil {
		b.Fatal(err)
	}

	defer machine.Close()

	payload := bytes.Repeat([]byte("a"), 32)
	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = machine.Write(payload)
	}
}

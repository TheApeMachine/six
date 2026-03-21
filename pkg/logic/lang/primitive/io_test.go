package primitive

import (
	"bytes"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestValueIORead serializes one Value into a byte slice that decodes back to the same shell.
*/
func TestValueIORead(t *testing.T) {
	gc.Convey("Given a value with populated shell state", t, func() {
		value := BaseValue('R')
		value.SetStatePhase(11)
		value.SetAffine(3, 7)

		buffer := make([]byte, 4096)

		n, err := (&value).Read(buffer)
		gc.So(err, gc.ShouldBeNil)
		gc.So(n, gc.ShouldBeGreaterThan, 0)

		decodedMsg, err := capnp.NewDecoder(bytes.NewReader(buffer[:n])).Decode()
		gc.So(err, gc.ShouldBeNil)

		decoded, err := ReadRootValue(decodedMsg)
		gc.So(err, gc.ShouldBeNil)

		for index := range 8 {
			gc.So(decoded.Block(index), gc.ShouldEqual, value.Block(index))
		}
	})
}

/*
TestValueIOWrite decodes an incoming Value message into the receiver shell.
*/
func TestValueIOWrite(t *testing.T) {
	gc.Convey("Given an encoded incoming value", t, func() {
		source := BaseValue('W')
		source.SetStatePhase(13)
		source.SetAffine(5, 9)

		var encoded bytes.Buffer
		msg, err := (&source).snapshotMessage()
		gc.So(err, gc.ShouldBeNil)

		err = capnp.NewEncoder(&encoded).Encode(msg)
		gc.So(err, gc.ShouldBeNil)

		destination, err := New()
		gc.So(err, gc.ShouldBeNil)

		n, err := (&destination).Write(encoded.Bytes())
		gc.So(err, gc.ShouldBeNil)
		gc.So(n, gc.ShouldEqual, encoded.Len())

		for index := range 8 {
			gc.So(destination.Block(index), gc.ShouldEqual, source.Block(index))
		}
	})
}

/*
BenchmarkValueIORead measures Value message serialization throughput.
*/
func BenchmarkValueIORead(b *testing.B) {
	value := BaseValue('B')
	value.SetStatePhase(17)
	buffer := make([]byte, 4096)

	b.ResetTimer()

	for b.Loop() {
		if _, err := (&value).Read(buffer); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}

/*
BenchmarkValueIOWrite measures Value message decode throughput.
*/
func BenchmarkValueIOWrite(b *testing.B) {
	source := BaseValue('C')
	source.SetStatePhase(19)

	var encoded bytes.Buffer
	msg, err := (&source).snapshotMessage()
	if err != nil {
		b.Fatalf("snapshot failed: %v", err)
	}

	if err := capnp.NewEncoder(&encoded).Encode(msg); err != nil {
		b.Fatalf("encode failed: %v", err)
	}

	destination, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	b.ResetTimer()

	for b.Loop() {
		if _, err := (&destination).Write(encoded.Bytes()); err != nil {
			b.Fatalf("write failed: %v", err)
		}
	}
}

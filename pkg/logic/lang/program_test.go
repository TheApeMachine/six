package lang

import (
	"bytes"
	"io"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
)

/*
TestProgramRead drains one serialized Program snapshot and then reaches EOF.
*/
func TestProgramRead(t *testing.T) {
	gc.Convey("Given a program with seeded values", t, func() {
		program, err := New()
		gc.So(err, gc.ShouldBeNil)

		values, err := primitive.NewValue_List(program.Segment(), 2)
		gc.So(err, gc.ShouldBeNil)

		first := primitive.BaseValue('A')
		first.SetStatePhase(3)
		firstDst := values.At(0)
		firstDst.CopyFrom(first)

		second := primitive.BaseValue('B')
		second.SetStatePhase(5)
		secondDst := values.At(1)
		secondDst.CopyFrom(second)

		err = program.SetValues(values)
		gc.So(err, gc.ShouldBeNil)

		buffer := make([]byte, 4096)

		n, err := program.Read(buffer)
		gc.So(err, gc.ShouldBeNil)
		gc.So(n, gc.ShouldBeGreaterThan, 0)

		decodedMsg, err := capnp.NewDecoder(bytes.NewReader(buffer[:n])).Decode()
		gc.So(err, gc.ShouldBeNil)

		decoded, err := ReadRootProgram(decodedMsg)
		gc.So(err, gc.ShouldBeNil)

		decodedValues, err := decoded.Values()
		gc.So(err, gc.ShouldBeNil)
		gc.So(decodedValues.Len(), gc.ShouldEqual, 2)

		nextN, nextErr := program.Read(buffer)
		gc.So(nextN, gc.ShouldEqual, 0)
		gc.So(nextErr, gc.ShouldEqual, io.EOF)
	})
}

/*
TestProgramWrite merges incoming Program values into the receiver Program.
*/
func TestProgramWrite(t *testing.T) {
	gc.Convey("Given a receiver and an encoded incoming program", t, func() {
		program, err := New()
		gc.So(err, gc.ShouldBeNil)

		existing, err := primitive.NewValue_List(program.Segment(), 1)
		gc.So(err, gc.ShouldBeNil)

		base := primitive.BaseValue('P')
		base.SetStatePhase(7)
		existingDst := existing.At(0)
		existingDst.CopyFrom(base)

		err = program.SetValues(existing)
		gc.So(err, gc.ShouldBeNil)

		incoming, err := New()
		gc.So(err, gc.ShouldBeNil)

		incomingValues, err := primitive.NewValue_List(incoming.Segment(), 1)
		gc.So(err, gc.ShouldBeNil)

		appended := primitive.BaseValue('Q')
		appended.SetStatePhase(11)
		incomingDst := incomingValues.At(0)
		incomingDst.CopyFrom(appended)

		err = incoming.SetValues(incomingValues)
		gc.So(err, gc.ShouldBeNil)

		var encoded bytes.Buffer
		msg, err := incoming.snapshotMessage()
		gc.So(err, gc.ShouldBeNil)

		err = capnp.NewEncoder(&encoded).Encode(msg)
		gc.So(err, gc.ShouldBeNil)

		n, err := program.Write(encoded.Bytes())
		gc.So(err, gc.ShouldBeNil)
		gc.So(n, gc.ShouldEqual, encoded.Len())

		merged, err := program.Values()
		gc.So(err, gc.ShouldBeNil)
		gc.So(merged.Len(), gc.ShouldEqual, 2)

		for index := range 8 {
			gc.So(merged.At(0).Block(index), gc.ShouldEqual, base.Block(index))
			gc.So(merged.At(1).Block(index), gc.ShouldEqual, appended.Block(index))
		}
	})
}

/*
BenchmarkProgramRead measures Program snapshot serialization throughput.
*/
func BenchmarkProgramRead(b *testing.B) {
	program, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	values, err := primitive.NewValue_List(program.Segment(), 2)
	if err != nil {
		b.Fatalf("value list allocation failed: %v", err)
	}

	firstDst := values.At(0)
	firstDst.CopyFrom(primitive.BaseValue('L'))

	secondDst := values.At(1)
	secondDst.CopyFrom(primitive.BaseValue('M'))

	if err := program.SetValues(values); err != nil {
		b.Fatalf("set values failed: %v", err)
	}

	buffer := make([]byte, 4096)

	b.ResetTimer()

	for b.Loop() {
		if err := program.refreshBuffer(); err != nil {
			b.Fatalf("refresh buffer failed: %v", err)
		}

		if _, err := program.Read(buffer); err != nil && err != io.EOF {
			b.Fatalf("read failed: %v", err)
		}
	}
}

/*
BenchmarkProgramWrite measures Program append decode throughput.
*/
func BenchmarkProgramWrite(b *testing.B) {
	incoming, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	incomingValues, err := primitive.NewValue_List(incoming.Segment(), 1)
	if err != nil {
		b.Fatalf("incoming list allocation failed: %v", err)
	}

	incomingDst := incomingValues.At(0)
	incomingDst.CopyFrom(primitive.BaseValue('N'))

	if err := incoming.SetValues(incomingValues); err != nil {
		b.Fatalf("set incoming values failed: %v", err)
	}

	var encoded bytes.Buffer
	msg, err := incoming.snapshotMessage()
	if err != nil {
		b.Fatalf("snapshot failed: %v", err)
	}

	if err := capnp.NewEncoder(&encoded).Encode(msg); err != nil {
		b.Fatalf("encode failed: %v", err)
	}

	b.ResetTimer()

	for b.Loop() {
		program, allocErr := New()
		if allocErr != nil {
			b.Fatalf("allocation failed: %v", allocErr)
		}

		if _, err := program.Write(encoded.Bytes()); err != nil {
			b.Fatalf("write failed: %v", err)
		}
	}
}

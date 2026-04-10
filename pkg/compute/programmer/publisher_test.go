package programmer

import (
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

type fakeSubmitQueue struct {
	submits  atomic.Int64
	compiled atomic.Int32
}

func (fake *fakeSubmitQueue) SubmitTracked(task func()) {
	fake.submits.Add(1)

	if task != nil {
		task()
	}
}

func (fake *fakeSubmitQueue) CompileAndExecute(program *Compiler) error {
	fake.compiled.Add(1)

	_ = program

	return nil
}

func TestNewPublisher(t *testing.T) {
	Convey("NewPublisher rejects nil arguments", t, func() {
		fake := &fakeSubmitQueue{}
		sink := &countSink{}

		_, err := NewPublisher(nil, stubFactory, sink)
		So(err, ShouldNotBeNil)

		_, err = NewPublisher(fake, nil, sink)
		So(err, ShouldNotBeNil)

		_, err = NewPublisher(fake, stubFactory, nil)
		So(err, ShouldNotBeNil)
	})

	Convey("NewPublisher runs factory, compile path, and sink per chunk", t, func() {
		fake := &fakeSubmitQueue{}
		sink := &countSink{}

		publisher, err := NewPublisher(fake, stubFactory, sink)
		So(err, ShouldBeNil)

		value, valErr := primitive.FirstSegment(primitive.NewValue([]byte{1, 2, 3}))
		So(valErr, ShouldBeNil)

		defer value.Close()

		So(publisher.Publish(value, "l"), ShouldBeNil)
		So(fake.submits.Load(), ShouldEqual, 1)
		So(fake.compiled.Load(), ShouldEqual, 1)
		So(sink.n, ShouldEqual, 1)
	})
}

func stubFactory(
	value *primitive.Value,
	_ string,
) (*Compiler, error) {
	frame := *value

	return New(&frame, CompilerWithFinalizer(Finalizer(func(
		v *primitive.Value,
		_ FinalizeNext,
	) ([]*primitive.Value, error) {
		if v == nil {
			return nil, nil
		}

		out, err := primitive.FirstSegment(primitive.NewValue([]byte{byte(v[0])}))

		if err != nil {
			return nil, err
		}

		return []*primitive.Value{out}, nil
	}))), nil
}

type countSink struct {
	n int
}

func (sink *countSink) Publish(value *primitive.Value, _ string) error {
	if value == nil {
		return nil
	}

	sink.n++

	return nil
}

func BenchmarkNewPublisher_Publish(b *testing.B) {
	fake := &fakeSubmitQueue{}
	sink := &countSink{}

	publisher, err := NewPublisher(fake, stubFactory, sink)

	if err != nil {
		b.Fatal(err)
	}

	value, valErr := primitive.FirstSegment(primitive.NewValue(make([]byte, 32)))

	if valErr != nil {
		b.Fatal(valErr)
	}

	defer value.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		sink.n = 0
		fake.submits.Store(0)
		fake.compiled.Store(0)

		if pubErr := publisher.Publish(value, ""); pubErr != nil {
			b.Fatal(pubErr)
		}
	}
}

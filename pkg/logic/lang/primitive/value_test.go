package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
TestValueNew verifies New allocates a valid zero value.
*/
func TestValueNew(t *testing.T) {
	gc.Convey("Given a call to New", t, func() {
		value, err := New()

		gc.So(err, gc.ShouldBeNil)
		gc.So(value.IsValid(), gc.ShouldBeTrue)
		gc.So(value.ActiveCount(), gc.ShouldEqual, 0)
	})
}

/*
TestValueNeutralValue verifies NeutralValue initializes identity affine state.
*/
func TestValueNeutralValue(t *testing.T) {
	gc.Convey("Given a neutral value", t, func() {
		value := NeutralValue()
		scale, translate := value.Affine()

		gc.So(scale, gc.ShouldEqual, 1)
		gc.So(translate, gc.ShouldEqual, 0)
	})
}

/*
TestValueBlockAndCopyFrom verifies block extraction and copying.
*/
func TestValueBlockAndCopyFrom(t *testing.T) {
	gc.Convey("Given source and destination values", t, func() {
		source, err := New()
		gc.So(err, gc.ShouldBeNil)

		source.SetC0(11)
		source.SetC1(22)
		source.SetC2(33)
		source.SetC3(44)
		source.SetC4(55)
		source.SetC5(66)
		source.SetC6(77)
		source.SetC7(88)

		destination, err := New()
		gc.So(err, gc.ShouldBeNil)
		destination.CopyFrom(source)

		for index := range 8 {
			gc.So(destination.Block(index), gc.ShouldEqual, source.Block(index))
		}

		gc.So(destination.Block(8), gc.ShouldEqual, 0)
	})
}

/*
TestValueSliceListRoundTrip verifies list conversion retains bit state.
*/
func TestValueSliceListRoundTrip(t *testing.T) {
	gc.Convey("Given a value slice converted to list and back", t, func() {
		first, err := New()
		gc.So(err, gc.ShouldBeNil)
		first.Set(3)
		first.SetStatePhase(5)

		second, err := New()
		gc.So(err, gc.ShouldBeNil)
		second.Set(9)
		second.SetStatePhase(11)

		in := []Value{first, second, NeutralValue()}

		list, err := ValueSliceToList(in)
		gc.So(err, gc.ShouldBeNil)

		out, err := ValueListToSlice(list)
		gc.So(err, gc.ShouldBeNil)
		gc.So(len(out), gc.ShouldEqual, len(in))

		for i := range in {
			for blockIndex := range 8 {
				gc.So(out[i].Block(blockIndex), gc.ShouldEqual, in[i].Block(blockIndex))
			}
		}
	})
}

/*
BenchmarkValueSliceToList measures value-list packing throughput.
*/
func BenchmarkValueSliceToList(b *testing.B) {
	values := make([]Value, 32)
	for index := range values {
		value, err := New()
		if err != nil {
			b.Fatalf("allocation failed: %v", err)
		}

		value.Set(index % 257)
		value.SetStatePhase(numeric.Phase((index % 256) + 1))
		values[index] = value
	}

	b.ResetTimer()

	for b.Loop() {
		if _, err := ValueSliceToList(values); err != nil {
			b.Fatalf("pack failed: %v", err)
		}
	}
}

/*
BenchmarkValueListToSlice measures value-list unpacking throughput.
*/
func BenchmarkValueListToSlice(b *testing.B) {
	values := make([]Value, 32)
	for index := range values {
		value, err := New()
		if err != nil {
			b.Fatalf("allocation failed: %v", err)
		}

		value.Set(index % 257)
		value.SetStatePhase(numeric.Phase((index % 256) + 1))
		values[index] = value
	}

	list, err := ValueSliceToList(values)
	if err != nil {
		b.Fatalf("pack failed: %v", err)
	}

	b.ResetTimer()

	for b.Loop() {
		if _, decodeErr := ValueListToSlice(list); decodeErr != nil {
			b.Fatalf("unpack failed: %v", decodeErr)
		}
	}
}

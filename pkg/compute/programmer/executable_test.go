package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewExecutable parses firmware source into tokens held for Compile.
*/
func TestNewExecutable(t *testing.T) {
	Convey("Given a minted Value and inline firmware", t, func() {
		values, err := primitive.NewValue([]byte("exec"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		source := "tokens tokens signals xor accumulate\n"

		executable := NewExecutable(value, source, nil)

		Convey("NewExecutable should parse tokens and retain the value handle", func() {
			So(executable, ShouldNotBeNil)
			So(executable.value, ShouldEqual, value)
			So(len(executable.tokens), ShouldEqual, 1)
			So(executable.tokens[0].Op, ShouldEqual, XOR)
		})

		Reset(func() {
			value.Close()
		})
	})
}

/*
TestExecutable_Compile lowers stored tokens through the Compiler.
*/
func TestExecutable_Compile(t *testing.T) {
	Convey("Given an executable built from xor firmware", t, func() {
		values, err := primitive.NewValue([]byte("compile"))

		So(err, ShouldBeNil)

		value := values[0]
		source := "tokens tokens signals xor accumulate\n"

		executable := NewExecutable(value, source, nil)

		Convey("Compile should emit frames matching direct Compiler output", func() {
			frames, err := executable.Compile(CPU)

			So(err, ShouldBeNil)
			So(len(frames), ShouldEqual, 1)
			So(frames[0].Program[0], ShouldEqual, uint64(XOR)&0xF)
		})

		Reset(func() {
			value.Close()
		})
	})
}

/*
TestExecutable_ApplyContinuation verifies that trailing scheduler directives
materialize into the in-value scheduling word rather than being handled only by
Go-side callbacks.
*/
func TestExecutable_ApplyContinuation(t *testing.T) {
	Convey("Given an executable built from firmware with next self", t, func() {
		values, err := primitive.NewValue([]byte("continue"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		source := "tokens tokens signals xor accumulate\nnext self\n"

		executable := NewExecutable(value, source, nil)

		Convey("ApplyContinuation should schedule the current Value ID in word 117", func() {
			executable.ApplyContinuation()

			So(value[kernel.SchedulingNextProgramWord], ShouldEqual, value.ID())
		})

		Reset(func() {
			value.Close()
		})
	})
}

func BenchmarkNewExecutable(b *testing.B) {
	values, err := primitive.NewValue([]byte("bench executable"))

	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}

	value := values[0]
	defer value.Close()

	source := "tokens tokens signals xor accumulate\n"

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = NewExecutable(value, source, nil)
	}
}

func BenchmarkExecutable_Compile(b *testing.B) {
	values, err := primitive.NewValue([]byte("bench compile"))

	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}

	value := values[0]
	defer value.Close()

	source := "tokens tokens signals xor accumulate\n"
	executable := NewExecutable(value, source, nil)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = executable.Compile(CPU)
	}
}

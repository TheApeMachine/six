package programmer

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestNewExecutable(t *testing.T) {
	Convey("Given a compiler and nil finalizer", t, func() {
		compiler := NewCompiler([]Token{})
		executable := NewExecutable(compiler, nil)

		Convey("NewExecutable should wire the compiler", func() {
			So(executable.compiler, ShouldEqual, compiler)
			So(executable.finalizer, ShouldBeNil)
		})
	})
}

func TestExecutable_Inputs(t *testing.T) {
	Convey("Given WithInputs", t, func() {
		compiler := NewCompiler([]Token{})
		ingress := &primitive.Value{}
		executable := NewExecutable(compiler, nil).WithInputs([]*primitive.Value{ingress})

		Convey("Inputs should return the attached slice", func() {
			So(executable.Inputs(), ShouldResemble, []*primitive.Value{ingress})
		})
	})
}

func TestExecutable_WithInputs(t *testing.T) {
	Convey("Given an Executable", t, func() {
		compiler := NewCompiler([]Token{})
		executable := NewExecutable(compiler, nil)
		ingress := &primitive.Value{}

		Convey("WithInputs should return the same Executable for chaining", func() {
			out := executable.WithInputs([]*primitive.Value{ingress})

			So(out, ShouldEqual, executable)
			So(executable.inputs, ShouldResemble, []*primitive.Value{ingress})
		})
	})
}

func TestExecutable_Execute(t *testing.T) {
	original := *core.Cfg

	t.Cleanup(func() {
		*core.Cfg = original
	})

	progStart := core.Cfg.Value.Region.Program.Start
	idWord := core.Cfg.Value.Region.ID.Start

	Convey("Given a parsed program with next and a compiler", t, func() {
		src := `tokens[0,1] tokens[1,1] signals[0,1] xor accumulate
next 99
`
		toks, cont, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)

		compiler := NewCompiler(toks, WithContinuation(cont))
		executable := NewExecutable(compiler, nil)

		Convey("Execute with CPU should return one full-Value pointer with program and scheduling words", func() {
			ptrs, err := executable.Execute(CPU)

			So(err, ShouldBeNil)
			So(len(ptrs), ShouldEqual, 1)

			frameWords := (*[128]uint64)(ptrs[0])

			So(frameWords[progStart], ShouldEqual, uint64(XOR&0xF))
			So(frameWords[kernel.SchedulingNextProgramWord], ShouldEqual, uint64(99))
		})

		Convey("Execute with Metal should materialize the same program low nibble", func() {
			ptrs, err := executable.Execute(Metal)

			So(err, ShouldBeNil)
			So(len(ptrs), ShouldEqual, 1)

			frameWords := (*[128]uint64)(ptrs[0])

			So(frameWords[progStart], ShouldEqual, uint64(XOR&0xF))
		})

		Convey("Execute with CUDA should materialize the same program low nibble", func() {
			ptrs, err := executable.Execute(CUDA)

			So(err, ShouldBeNil)

			frameWords := (*[128]uint64)(ptrs[0])

			So(frameWords[progStart], ShouldEqual, uint64(XOR&0xF))
		})
	})

	Convey("Given ingress Value with a stamped ID", t, func() {
		src := `tokens[0,1] tokens[1,1] signals[0,1] xor accumulate`
		toks, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)

		compiler := NewCompiler(toks)
		ingress := &primitive.Value{}
		ingress.Set(idWord, 5555)

		executable := NewExecutable(compiler, nil).WithInputs([]*primitive.Value{ingress})

		Convey("Execute should copy the ingress wire including ID", func() {
			ptrs, err := executable.Execute(CPU)

			So(err, ShouldBeNil)

			frameWords := (*[128]uint64)(ptrs[0])

			So(frameWords[idWord], ShouldEqual, uint64(5555))
		})
	})

	Convey("Given ingress with populated token words", t, func() {
		src := `tokens[0,2] tokens[2,2] signals[0,1] xor accumulate`
		toks, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)

		compiler := NewCompiler(toks)
		ingress := &primitive.Value{}
		tokStart := core.Cfg.Value.Region.Tokens.Start
		ingress.Set(tokStart+0, 0x1122334455667788)
		ingress.Set(tokStart+1, 0xAABBCCDDEEFF0011)
		ingress.Set(tokStart+2, 0xCAFEBABE00001111)
		ingress.Set(tokStart+3, 0xDEADBEEFDEADBEEF)

		executable := NewExecutable(compiler, nil).WithInputs([]*primitive.Value{ingress})

		Convey("Execute should preserve tokens untouched — operand staging is gone", func() {
			ptrs, err := executable.Execute(CPU)

			So(err, ShouldBeNil)

			frameWords := (*[128]uint64)(ptrs[0])

			So(frameWords[tokStart+0], ShouldEqual, uint64(0x1122334455667788))
			So(frameWords[tokStart+1], ShouldEqual, uint64(0xAABBCCDDEEFF0011))
			So(frameWords[tokStart+2], ShouldEqual, uint64(0xCAFEBABE00001111))
			So(frameWords[tokStart+3], ShouldEqual, uint64(0xDEADBEEFDEADBEEF))
		})

		Convey("Execute should pack srcA / srcB / dst into the program region lanes", func() {
			ptrs, err := executable.Execute(CPU)

			So(err, ShouldBeNil)

			frameWords := (*[128]uint64)(ptrs[0])

			sigStart := core.Cfg.Value.Region.Signals.Start

			So(frameWords[kernel.ProgramSrcAWord], ShouldEqual,
				kernel.PackRegionRef(tokStart+0, 2))
			So(frameWords[kernel.ProgramSrcBWord], ShouldEqual,
				kernel.PackRegionRef(tokStart+2, 2))
			So(frameWords[kernel.ProgramDstWord], ShouldEqual,
				kernel.PackRegionRef(sigStart+0, 1))
		})
	})

	Convey("Given two operation lines", t, func() {
		src := `tokens[0,1] tokens[1,1] signals[0,1] xor accumulate
tokens[0,1] tokens[1,1] signals[0,1] and reduce
`
		toks, _, err := NewParser(NewProgram(src)).Parse()

		So(err, ShouldBeNil)

		compiler := NewCompiler(toks)
		executable := NewExecutable(compiler, nil)

		Convey("Execute should return distinct pointers per frame", func() {
			ptrs, err := executable.Execute(CPU)

			So(err, ShouldBeNil)
			So(len(ptrs), ShouldEqual, 2)
			So(ptrs[0], ShouldNotEqual, ptrs[1])
		})
	})
}

func TestExecutable_valueForFrame(t *testing.T) {
	Convey("Given Executable without inputs", t, func() {
		compiler := NewCompiler([]Token{})
		executable := NewExecutable(compiler, nil)

		Convey("valueForFrame should return a zero Value pointer", func() {
			v := executable.valueForFrame()

			So(v, ShouldNotBeNil)
			So((*v)[0], ShouldEqual, uint64(0))
		})
	})

	Convey("Given Executable WithInputs", t, func() {
		compiler := NewCompiler([]Token{})
		ingress := &primitive.Value{}
		(*ingress)[3] = 42
		executable := NewExecutable(compiler, nil).WithInputs([]*primitive.Value{ingress})

		Convey("valueForFrame should copy the first ingress", func() {
			v := executable.valueForFrame()

			So((*v)[3], ShouldEqual, uint64(42))
		})
	})
}

func TestExecutable_Finalize(t *testing.T) {
	Convey("Given Executable without finalizer", t, func() {
		compiler := NewCompiler([]Token{})
		executable := NewExecutable(compiler, nil)
		var out primitive.Value

		Convey("Finalize should return the same Value in a slice", func() {
			vals, err := executable.Finalize(&out)

			So(err, ShouldBeNil)
			So(len(vals), ShouldEqual, 1)
			So(vals[0], ShouldEqual, &out)
		})
	})

	Convey("Given Executable with finalizer", t, func() {
		compiler := NewCompiler([]Token{})
		calls := 0

		executable := NewExecutable(compiler, func(v *primitive.Value) ([]*primitive.Value, error) {
			calls++

			return []*primitive.Value{v, v}, nil
		})

		var out primitive.Value

		Convey("Finalize should invoke the finalizer", func() {
			vals, err := executable.Finalize(&out)

			So(err, ShouldBeNil)
			So(calls, ShouldEqual, 1)
			So(len(vals), ShouldEqual, 2)
		})
	})
}

func BenchmarkExecutable_Execute(b *testing.B) {
	original := *core.Cfg

	b.Cleanup(func() {
		*core.Cfg = original
	})

	src := `tokens[0,1] tokens[1,1] signals[0,1] xor accumulate`
	toks, _, _ := NewParser(NewProgram(src)).Parse()
	compiler := NewCompiler(toks)
	executable := NewExecutable(compiler, nil)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		ptrs, err := executable.Execute(CPU)

		if err != nil || len(ptrs) == 0 {
			b.Fatal(err)
		}

		_ = unsafe.Pointer(ptrs[0])
	}
}

package programmer

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
newTestToken is the common setup across every Run/Execute convey. It
parses a single five-field line so tests never hand-craft Token structs
(and therefore never drift from the parser's invariants).
*/
func newTestToken(t *testing.T, line string) Token {
	t.Helper()

	toks, _, err := NewParser(NewProgram(line)).Parse()

	if err != nil {
		t.Fatalf("parse %q: %v", line, err)
	}

	if len(toks) != 1 {
		t.Fatalf("parse %q: want 1 token, got %d", line, len(toks))
	}

	return toks[0]
}

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
			So(frameWords[schedulingNextProgramWord], ShouldEqual, uint64(99))
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

		Convey("Execute should stage srcA words into the A operand lanes", func() {
			ptrs, err := executable.Execute(CPU)

			So(err, ShouldBeNil)

			frameWords := (*[128]uint64)(ptrs[0])

			So(frameWords[0], ShouldEqual, uint64(0x1122334455667788))
			So(frameWords[1], ShouldEqual, uint64(0xAABBCCDDEEFF0011))
		})

		Convey("Execute should tile srcB words across the B rotation lanes", func() {
			ptrs, err := executable.Execute(CPU)

			So(err, ShouldBeNil)

			frameWords := (*[128]uint64)(ptrs[0])

			So(frameWords[32], ShouldEqual, uint64(0xCAFEBABE00001111))
			So(frameWords[33], ShouldEqual, uint64(0xDEADBEEFDEADBEEF))
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

func TestOperandBands_stage(t *testing.T) {
	original := *core.Cfg

	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given operandBands and a Value with token words populated", t, func() {
		bands := newOperandBands()
		var value primitive.Value
		tokStart := core.Cfg.Value.Region.Tokens.Start
		value.Set(tokStart+0, 0xDEAD)
		value.Set(tokStart+1, 0xBEEF)

		tok := newTestToken(t, "tokens[0,2] tokens[0,2] signals[0,1] xor accumulate")

		Convey("stage should copy srcA words into lanes 0..1", func() {
			bands.stage(&value, tok)

			So(value[0], ShouldEqual, uint64(0xDEAD))
			So(value[1], ShouldEqual, uint64(0xBEEF))
		})

		Convey("stage should tile srcB words across every rotation", func() {
			bands.stage(&value, tok)

			for rotation := 0; rotation < 16; rotation++ {
				So(value[bWordBase+rotation*bRotationWords+0], ShouldEqual, uint64(0xDEAD))
				So(value[bWordBase+rotation*bRotationWords+1], ShouldEqual, uint64(0xBEEF))
			}
		})
	})

	Convey("Given a srcA span wider than the A lane", t, func() {
		bands := newOperandBands()
		var value primitive.Value
		tokStart := core.Cfg.Value.Region.Tokens.Start

		for idx := 0; idx < 8; idx++ {
			value.Set(tokStart+idx, uint64(1)<<uint(idx*8))
		}

		tok := newTestToken(t, "tokens[0,8] tokens[0,8] signals[0,1] xor accumulate")

		Convey("stage should XOR-fold the overflow into the 4-word A lane", func() {
			bands.stage(&value, tok)

			expected0 := (uint64(1) << 0) ^ (uint64(1) << 32)
			expected1 := (uint64(1) << 8) ^ (uint64(1) << 40)
			expected2 := (uint64(1) << 16) ^ (uint64(1) << 48)
			expected3 := (uint64(1) << 24) ^ (uint64(1) << 56)

			So(value[0], ShouldEqual, expected0)
			So(value[1], ShouldEqual, expected1)
			So(value[2], ShouldEqual, expected2)
			So(value[3], ShouldEqual, expected3)
		})
	})
}

func TestOperandBands_writeback(t *testing.T) {
	original := *core.Cfg

	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given signal words set by a substrate pass", t, func() {
		bands := newOperandBands()
		var value primitive.Value
		sigStart := core.Cfg.Value.Region.Signals.Start
		value.Set(sigStart+0, 0x00FF00FF00FF00FF)
		value.Set(sigStart+1, 0xAA55AA55AA55AA55)

		Convey("writeback with accumulate should XOR signals into dst span", func() {
			tok := newTestToken(t, "tokens[0,1] tokens[0,1] affinity[0,2] xor accumulate")
			affStart := core.Cfg.Value.Region.Affinity.Start
			value.Set(affStart+0, 0x0F0F0F0F0F0F0F0F)
			value.Set(affStart+1, 0x1111111111111111)

			bands.writeback(&value, tok)

			So(value[affStart+0], ShouldEqual, uint64(0x0F0F0F0F0F0F0F0F)^uint64(0x00FF00FF00FF00FF))
			So(value[affStart+1], ShouldEqual, uint64(0x1111111111111111)^uint64(0xAA55AA55AA55AA55))
		})

		Convey("writeback with reduce should popcount signals into dst[0]", func() {
			tok := newTestToken(t, "tokens[0,1] tokens[0,1] signals[0,1] xor reduce")

			bands.writeback(&value, tok)

			expected := uint64(32 + 32)

			So(value[sigStart+0], ShouldEqual, expected)
		})
	})
}

func TestOperandBands_clearSignals(t *testing.T) {
	Convey("Given a Value with signal words set", t, func() {
		bands := newOperandBands()
		var value primitive.Value
		sigStart := core.Cfg.Value.Region.Signals.Start
		sigWords := int((core.Cfg.Value.Region.Signals.Bits + 63) / 64)

		for idx := 0; idx < sigWords; idx++ {
			value.Set(sigStart+idx, ^uint64(0))
		}

		Convey("clearSignals should zero every signal word", func() {
			bands.clearSignals(&value)

			for idx := 0; idx < sigWords; idx++ {
				So(value[sigStart+idx], ShouldEqual, uint64(0))
			}
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

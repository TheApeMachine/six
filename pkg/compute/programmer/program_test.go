package programmer

import (
	"testing"

	"github.com/theapemachine/six/pkg/core"

	. "github.com/smartystreets/goconvey/convey"
)

/*
TestNewProgram resolves named firmware from config when present, otherwise treats the argument as source text.
*/
func TestNewProgram(t *testing.T) {
	Convey("Given an inline program body that is not a config key", t, func() {
		body := "tokens tokens signals xor accumulate\n"
		program := NewProgram(body)

		Convey("NewProgram should keep that text as the source", func() {
			So(program, ShouldNotBeNil)
			So(program.source, ShouldEqual, body)
		})
	})

	Convey("Given the affinity program name from config", t, func() {
		body, ok := core.Cfg.Programs["affinity"]

		if !ok || body == "" {
			t.Skip("Cfg.Programs affinity body missing; load cmd/cfg/config.yml for this case")
		}

		program := NewProgram("affinity")

		Convey("NewProgram should resolve the named body from Cfg.Programs", func() {
			So(program.source, ShouldEqual, body)
		})
	})
}

/*
TestProgram_Load splits non-empty lines into fields and strips inline hash comments.
*/
func TestProgram_Load(t *testing.T) {
	Convey("Given multi-line source with blanks and inline comments", t, func() {
		source := "  tokens  tokens  signals  xor  accumulate  # note\n\ncontext context gradient or reduce\n"
		program := NewProgram(source)

		Convey("Load should return trimmed fields per logical line", func() {
			lines := program.Load()

			So(len(lines), ShouldEqual, 2)
			So(lines[0][0], ShouldEqual, "tokens")
			So(lines[0][4], ShouldEqual, "accumulate")
			So(lines[1][3], ShouldEqual, "or")
		})

		Convey("a second Load should return the cached rows without re-splitting", func() {
			again := program.Load()

			So(again, ShouldResemble, program.lineFields)
		})
	})

	Convey("Given empty source", t, func() {
		program := NewProgram("")

		Convey("Load should return an empty row list", func() {
			So(len(program.Load()), ShouldEqual, 0)
		})
	})
}

/*
TestProgram_ResetParseState clears cached rows so Load re-tokenizes.
*/
func TestProgram_ResetParseState(t *testing.T) {
	Convey("Given a program after Load", t, func() {
		program := NewProgram("tokens tokens signals xor accumulate\n")
		first := program.Load()

		So(len(first), ShouldBeGreaterThan, 0)

		Convey("ResetParseState should allow Load to rebuild rows", func() {
			program.ResetParseState()

			So(program.lineFields, ShouldBeNil)

			second := program.Load()

			So(len(second), ShouldEqual, len(first))
		})
	})

	Convey("Given a nil Program pointer", t, func() {
		var program *Program

		Convey("ResetParseState should not panic", func() {
			program.ResetParseState()
		})
	})
}

func BenchmarkProgram_Load(b *testing.B) {
	source := "tokens tokens signals xor accumulate\ncontext context gradient or reduce\n"
	program := NewProgram(source)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		program.ResetParseState()
		_ = program.Load()
	}
}

func BenchmarkProgram_NewProgram(b *testing.B) {
	body := "tokens tokens signals xor accumulate\n"

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = NewProgram(body)
	}
}

package programmer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestNewProgram(t *testing.T) {
	original := *core.Cfg
	t.Cleanup(func() {
		*core.Cfg = original
	})

	Convey("Given a restored core.Cfg after the test", t, func() {
		Convey("When Programs maps the key to non-empty text", func() {
			core.Cfg.Programs = map[string]string{
				"named": "from config",
			}

			program := NewProgram("named")

			Convey("NewProgram should use the config body", func() {
				So(program.source, ShouldEqual, "from config")
			})
		})

		Convey("When the key is missing", func() {
			core.Cfg.Programs = map[string]string{
				"other": "x",
			}

			program := NewProgram("inline body")

			Convey("NewProgram should use the argument as source", func() {
				So(program.source, ShouldEqual, "inline body")
			})
		})

		Convey("When the key maps to empty string", func() {
			core.Cfg.Programs = map[string]string{
				"empty": "",
			}

			program := NewProgram("empty")

			Convey("NewProgram should fall back to the argument", func() {
				So(program.source, ShouldEqual, "empty")
			})
		})
	})
}

func TestLoad(t *testing.T) {
	Convey("Load should split into trimmed field rows (five columns per line)", t, func() {
		program := NewProgram("  tokens[0] tokens[1] signals[0] xor accumulate  \n\n  affinity[0] signals[0] affinity[0] popcount reduce  ")

		lines := program.Load()

		So(lines, ShouldResemble, [][]string{
			{"tokens[0]", "tokens[1]", "signals[0]", "xor", "accumulate"},
			{"affinity[0]", "signals[0]", "affinity[0]", "popcount", "reduce"},
		})
	})

	Convey("Load on empty source", t, func() {
		program := NewProgram("")

		So(program.Load(), ShouldResemble, [][]string{})
	})

	Convey("Load caches the same slice", t, func() {
		program := NewProgram("tokens[0] tokens[1] signals[0] xor accumulate")

		first := program.Load()
		second := program.Load()

		So(first, ShouldEqual, second)
	})
}

func BenchmarkLoad(b *testing.B) {
	src := "tokens[0] tokens[1] signals[0] xor accumulate\naffinity[0] signals[0] affinity[0] popcount reduce\n"
	program := NewProgram(src)

	b.ResetTimer()

	for range b.N {
		program.lineFields = nil
		_ = program.Load()
	}
}

func BenchmarkNewProgram(b *testing.B) {
	original := *core.Cfg
	b.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Programs = map[string]string{
		"benchkey": "bench body",
	}

	b.ResetTimer()

	for range b.N {
		_ = NewProgram("benchkey")
	}
}

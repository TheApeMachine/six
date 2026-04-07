package attention

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewContext(t *testing.T) {
	t.Parallel()

	Convey("NewContext keeps token and vocabulary references", t, func() {
		tokens := []string{"a", "b"}
		vocab := []string{"a", "b"}
		co := map[string]map[string]float64{
			"a": {"b": 1},
			"b": {"a": 1},
		}

		context := NewContext(tokens, vocab, co)

		So(context, ShouldNotBeNil)
		So(len(context.Run()), ShouldEqual, 2)
	})
}

func TestContextRun(t *testing.T) {
	t.Parallel()

	Convey("Run maps each token through semantic equivalence", t, func() {
		tokens := []string{"alpha", "alpha"}
		vocab := []string{"alpha", "beta"}
		co := map[string]map[string]float64{
			"alpha": {"beta": 0.9, "gamma": 0.1},
			"beta":  {"alpha": 0.8},
			"gamma": {"alpha": 0.2},
		}

		context := NewContext(tokens, vocab, co)
		out := context.Run()

		So(len(out), ShouldEqual, 2)
		So(out[0].Original, ShouldEqual, "alpha")
		So(out[1].Original, ShouldEqual, "alpha")
	})
}

func BenchmarkContextRun(b *testing.B) {
	tokens := make([]string, 32)

	for idx := range tokens {
		tokens[idx] = "tok"
	}

	vocab := []string{"tok", "other"}
	co := map[string]map[string]float64{
		"tok":   {"other": 0.5},
		"other": {"tok": 0.5},
	}

	context := NewContext(tokens, vocab, co)

	b.ResetTimer()

	for b.Loop() {
		_ = context.Run()
	}
}

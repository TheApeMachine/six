package process

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/provider"
)

// mockDataset emits a fixed sequence of RawTokens across two sample IDs.
type mockDataset struct {
	tokens []provider.RawToken
	pos    int
}

func newMockDataset(samples []string) *mockDataset {
	var tokens []provider.RawToken

	for id, sample := range samples {
		for _, ch := range []byte(sample) {
			tokens = append(tokens, provider.RawToken{
				SampleID: uint32(id),
				Symbol:   ch,
			})
		}
	}

	return &mockDataset{tokens: tokens}
}

func (mock *mockDataset) Generate() chan provider.RawToken {
	ch := make(chan provider.RawToken)

	go func() {
		defer close(ch)

		for _, tkn := range mock.tokens {
			ch <- tkn
		}
	}()

	return ch
}

func TestPrompt(t *testing.T) {
	Convey("Given a Prompt backed by a static string list", t, func() {
		Convey("When no holdout is configured", func() {
			prompt := NewPrompt(PromptWithStrings([]string{"Hello World"}))

			Convey("It should advance and expose the original unchanged", func() {
				So(prompt.Next(), ShouldBeTrue)
				So(prompt.Original(), ShouldEqual, "Hello World")
				So(prompt.Masked(), ShouldEqual, "Hello World")
			})
		})

		Convey("When the list is exhausted", func() {
			prompt := NewPrompt(PromptWithStrings([]string{"one", "two"}))

			So(prompt.Next(), ShouldBeTrue)
			So(prompt.Next(), ShouldBeTrue)

			Convey("It should return false on the third call", func() {
				So(prompt.Next(), ShouldBeFalse)
			})
		})

		Convey("When holdout type is RIGHT", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"abcdefghij"}),
				PromptWithHoldout(20, RIGHT),
			)

			Convey("It should mask the trailing 20 percent", func() {
				So(prompt.Next(), ShouldBeTrue)
				So(prompt.Original(), ShouldEqual, "abcdefghij")
				So(prompt.Masked(), ShouldEqual, "abcdefgh")
			})
		})

		Convey("When holdout type is TOP (alias for RIGHT)", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"abcdefghij"}),
				PromptWithHoldout(20, TOP),
			)

			Convey("It should produce the same result as RIGHT", func() {
				So(prompt.Next(), ShouldBeTrue)
				So(prompt.Masked(), ShouldEqual, "abcdefgh")
			})
		})

		Convey("When holdout type is LEFT", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"abcdefghij"}),
				PromptWithHoldout(20, LEFT),
			)

			Convey("It should mask the leading 20 percent", func() {
				So(prompt.Next(), ShouldBeTrue)
				So(prompt.Masked(), ShouldEqual, "cdefghij")
			})
		})

		Convey("When holdout type is BOTTOM (alias for LEFT)", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"abcdefghij"}),
				PromptWithHoldout(20, BOTTOM),
			)

			Convey("It should produce the same result as LEFT", func() {
				So(prompt.Next(), ShouldBeTrue)
				So(prompt.Masked(), ShouldEqual, "cdefghij")
			})
		})

		Convey("When holdout type is CENTER", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"abcdefghij"}),
				PromptWithHoldout(20, CENTER),
			)

			Convey("It should mask the middle 20 percent", func() {
				So(prompt.Next(), ShouldBeTrue)

				// 10 bytes, 20% → count=2, start=(10-2)/2=4
				// raw[:4]="abcd", raw[6:]="ghij" → "abcdghij"
				So(prompt.Masked(), ShouldEqual, "abcdghij")
			})
		})

		Convey("When holdout type is RANDOM", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"abcdefghij"}),
				PromptWithHoldout(30, RANDOM),
			)

			Convey("It should zero out 30 percent of bytes", func() {
				So(prompt.Next(), ShouldBeTrue)

				masked := prompt.Masked()
				zeroed := strings.Count(masked, "\x00")

				So(zeroed, ShouldEqual, 3)
				So(len(masked), ShouldEqual, 10)
			})
		})

		Convey("When holdout type is MATCH", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"Hello World Hello"}),
				PromptWithHoldout(0, MATCH),
				PromptWithMatch([]byte("Hello")),
			)

			Convey("It should replace all occurrences with zero bytes", func() {
				So(prompt.Next(), ShouldBeTrue)

				masked := []byte(prompt.Masked())

				So(masked[:5], ShouldResemble, make([]byte, 5))
				So(masked[12:], ShouldResemble, make([]byte, 5))
			})
		})

		Convey("When MATCH holdout has an empty pattern", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{"Hello"}),
				PromptWithHoldout(0, MATCH),
			)

			Convey("It should leave the string unchanged", func() {
				So(prompt.Next(), ShouldBeTrue)
				So(prompt.Masked(), ShouldEqual, "Hello")
			})
		})

		Convey("When the original is empty", func() {
			prompt := NewPrompt(
				PromptWithStrings([]string{""}),
				PromptWithHoldout(50, RIGHT),
			)

			Convey("It should produce an empty masked string", func() {
				So(prompt.Next(), ShouldBeTrue)
				So(prompt.Masked(), ShouldEqual, "")
			})
		})
	})

	Convey("Given a Prompt backed by a streaming Dataset", t, func() {
		samples := []string{"first sample", "second sample", "third"}
		prompt := NewPrompt(PromptWithDataset(newMockDataset(samples)))

		Convey("It should yield one sample per Next() call in order", func() {
			So(prompt.Next(), ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "first sample")

			So(prompt.Next(), ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "second sample")

			So(prompt.Next(), ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "third")

			So(prompt.Next(), ShouldBeFalse)
		})

		Convey("It should apply holdout masking to dataset samples", func() {
			prompt2 := NewPrompt(
				PromptWithDataset(newMockDataset([]string{"abcdefghij"})),
				PromptWithHoldout(50, RIGHT),
			)

			So(prompt2.Next(), ShouldBeTrue)
			So(prompt2.Masked(), ShouldEqual, "abcde")
		})
	})
}

func BenchmarkPromptNextStrings(b *testing.B) {
	samples := make([]string, 1000)

	for idx := range samples {
		samples[idx] = strings.Repeat("x", 512)
	}

	b.ResetTimer()

	for range b.N {
		prompt := NewPrompt(
			PromptWithStrings(samples),
			PromptWithHoldout(25, RIGHT),
		)

		for prompt.Next() {
		}
	}
}

func BenchmarkPromptNextDataset(b *testing.B) {
	samples := make([]string, 100)

	for idx := range samples {
		samples[idx] = strings.Repeat("y", 512)
	}

	b.ResetTimer()

	for range b.N {
		prompt := NewPrompt(
			PromptWithDataset(newMockDataset(samples)),
			PromptWithHoldout(25, RIGHT),
		)

		for prompt.Next() {
		}
	}
}

func BenchmarkApplyHoldoutRandom(b *testing.B) {
	prompt := NewPrompt(
		PromptWithHoldout(30, RANDOM),
	)
	prompt.original = strings.Repeat("z", 1024)

	b.ResetTimer()

	for range b.N {
		prompt.applyHoldout()
	}
}

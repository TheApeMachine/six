package process

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data/provider"
)

func TestApplyHoldout(t *testing.T) {
	tests := []struct {
		name     string
		original string
		heldout  Holdout
		wantMask string
	}{
		{"NONE no change", "hello", Holdout{Type: NONE}, "hello"},
		{"NONE percent ignored", "hello", Holdout{Type: NONE, Percent: 50}, "hello"},
		{"RIGHT mask trailing 40%", "abcdefghij", Holdout{Type: RIGHT, Percent: 40}, "abcdef"},
		{"RIGHT mask trailing 100%", "abc", Holdout{Type: RIGHT, Percent: 100}, ""},
		{"LEFT mask leading 30%", "abcdefghij", Holdout{Type: LEFT, Percent: 30}, "defghij"},
		{"LEFT mask leading 100%", "abc", Holdout{Type: LEFT, Percent: 100}, ""},
		{"CENTER mask middle 40%", "abcdefghij", Holdout{Type: CENTER, Percent: 40}, "abchij"},
		{"Percent clamped over 100 masks all for RIGHT", "abc", Holdout{Type: RIGHT, Percent: 150}, ""},
		{"Percent clamped under 0 no mask", "abc", Holdout{Type: RIGHT, Percent: -10}, "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Convey(tt.name, t, func() {
				prompt := NewPrompt()
				prompt.original = tt.original
				prompt.heldout = tt.heldout
				prompt.ApplyHoldout()
				So(prompt.Masked(), ShouldEqual, tt.wantMask)
				So(prompt.Original(), ShouldEqual, tt.original)
			})
		})
	}
}

func TestApplyHoldoutMatch(t *testing.T) {
	Convey("Given MATCH holdout with pattern", t, func() {
		prompt := NewPrompt()
		prompt.original = "axbxcx"
		prompt.heldout = Holdout{Type: MATCH, Percent: 0, Match: []byte("x")}
		prompt.ApplyHoldout()
		Convey("It should replace matching bytes with zeros", func() {
			So(prompt.Masked(), ShouldEqual, "a\x00b\x00c\x00")
		})
	})
}

func TestApplyHoldoutRandom(t *testing.T) {
	Convey("Given RANDOM holdout with fixed seed", t, func() {
		prompt := NewPrompt()
		prompt.original = "abcdefgh"
		prompt.heldout = Holdout{Type: RANDOM, Percent: 50}
		prompt.rng = newRandForTest(42)
		prompt.ApplyHoldout()
		Convey("It should mask approximately half the bytes", func() {
			masked := prompt.Masked()
			zeroCount := 0
			for _, b := range []byte(masked) {
				if b == 0 {
					zeroCount++
				}
			}
			So(zeroCount, ShouldEqual, 4)
		})
	})
}

func newRandForTest(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func TestNextFromStrings(t *testing.T) {
	Convey("Given a Prompt with PromptWithStrings", t, func() {
		prompts := []string{"first", "second", "third"}
		prompt := NewPrompt(PromptWithStrings(prompts))

		Convey("When Next is called repeatedly", func() {
			ok1 := prompt.Next()
			So(ok1, ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "first")

			ok2 := prompt.Next()
			So(ok2, ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "second")

			ok3 := prompt.Next()
			So(ok3, ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "third")

			ok4 := prompt.Next()
			Convey("It should exhaust and return false", func() {
				So(ok4, ShouldBeFalse)
			})
		})
	})
}

type mockDataset struct {
	tokens []provider.RawToken
}

func (m *mockDataset) Generate() chan provider.RawToken {
	ch := make(chan provider.RawToken, len(m.tokens)+1)
	for _, tkn := range m.tokens {
		ch <- tkn
	}
	close(ch)
	return ch
}

func TestNextFromDataset(t *testing.T) {
	Convey("Given a Prompt with mock dataset", t, func() {
		dataset := &mockDataset{
			tokens: []provider.RawToken{
				{SampleID: 1, Symbol: 'a'},
				{SampleID: 1, Symbol: 'b'},
				{SampleID: 1, Symbol: 'c'},
				{SampleID: 2, Symbol: 'x'},
				{SampleID: 2, Symbol: 'y'},
			},
		}
		prompt := NewPrompt(PromptWithDataset(dataset))

		Convey("When Next is called", func() {
			ok1 := prompt.Next()
			So(ok1, ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "abc")

			ok2 := prompt.Next()
			So(ok2, ShouldBeTrue)
			So(prompt.Original(), ShouldEqual, "xy")

			ok3 := prompt.Next()
			Convey("It should exhaust after sample grouping", func() {
				So(ok3, ShouldBeFalse)
			})
		})
	})
}

func TestPromptWithHoldout(t *testing.T) {
	Convey("Given PromptWithHoldout option", t, func() {
		prompt := NewPrompt(PromptWithHoldout(30, RIGHT))
		Convey("It should set heldout config", func() {
			So(prompt.heldout.Percent, ShouldEqual, 30)
			So(prompt.heldout.Type, ShouldEqual, RIGHT)
		})
	})
}

func TestPromptWithMatch(t *testing.T) {
	Convey("Given PromptWithMatch option", t, func() {
		match := []byte("sep")
		prompt := NewPrompt(PromptWithMatch(match))
		Convey("It should set heldout Match pattern", func() {
			So(prompt.heldout.Match, ShouldResemble, match)
		})
	})
}

func TestTokenizeMaskedNilTokenizer(t *testing.T) {
	Convey("Given a Prompt with nil tokenizer", t, func() {
		prompt := NewPrompt()
		prompt.masked = "test"
		Convey("When tokenizeMasked is called it should not panic", func() {
			prompt.tokenizeMasked()
			So(prompt.Error(), ShouldBeNil)
		})
	})
}

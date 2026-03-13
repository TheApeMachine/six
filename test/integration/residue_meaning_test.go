package integration

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/provider/local"
)

/*
TestUselessResidueDepletion tests exact corpus retrieval.
Each case loads a specific sentence, queries it exactly,
and asserts the system returns EXACTLY the expected content.
*/
func TestUselessResidueDepletion(t *testing.T) {
	cases := map[string]map[string]string{
		"exact Alice sentence": {
			"data":     "Alice was beginning to get very tired of sitting by her sister on the bank",
			"query":    "Alice was beginning to get very tired",
			"expected": "sitting by her sister on the bank",
		},
		"exact Rabbit sentence": {
			"data":     "but when the Rabbit actually took a watch out of its waistcoat pocket",
			"query":    "the Rabbit actually took a watch",
			"expected": "waistcoat pocket",
		},
		"exact Queen sentence": {
			"data":     "The Queen of Hearts she made some tarts all on a summer day",
			"query":    "The Queen of Hearts she made",
			"expected": "tarts",
		},
		"exact sleepy sentence": {
			"data":     "for the hot day made her feel very sleepy and stupid",
			"query":    "the hot day made her feel",
			"expected": "sleepy",
		},
		"exact daisies sentence": {
			"data":     "of getting up and picking the daisies when suddenly a White Rabbit",
			"query":    "getting up and picking the",
			"expected": "daisies",
		},
		"exact waistcoat sentence": {
			"data":     "with either a waistcoat pocket or a watch to take out of it",
			"query":    "a waistcoat pocket or a watch",
			"expected": "waistcoat",
		},
		"exact Oh dear sentence": {
			"data":     "Oh dear Oh dear I shall be late",
			"query":    "Oh dear Oh dear I shall be",
			"expected": "late",
		},
		"exact curiosity sentence": {
			"data":     "and burning with curiosity she ran across the field after it",
			"query":    "burning with curiosity she ran",
			"expected": "field",
		},
		"exact Off with her head": {
			"data":     "Off with her head the Queen shouted at the top of her voice",
			"query":    "the Queen shouted at the top of",
			"expected": "voice",
		},
		"exact nothing remarkable": {
			"data":     "There was nothing so very remarkable in that",
			"query":    "There was nothing so very",
			"expected": "remarkable",
		},
	}

	Convey("Given exact corpus data-query-expected triplets", t, func() {
		for name, tc := range cases {
			Convey(name, func() {
				helper := NewIntegrationHelper(
					context.Background(),
					local.New(local.WithStrings([]string{tc["data"]})),
				)
				defer helper.Teardown()

				results, err := helper.Machine.Prompt(
					helper.NewPrompt([]string{tc["query"]}),
				)
				So(err, ShouldBeNil)
				So(len(results), ShouldBeGreaterThan, 0)
				So(helper.ContainsExpected(results, tc["expected"]), ShouldBeTrue)
				t.Logf("%s: query=%q → expected=%q", name, tc["query"], tc["expected"])
			})
		}
	})
}

/*
BenchmarkUselessResidueDepletion benchmarks exact corpus retrieval.
*/
func BenchmarkUselessResidueDepletion(b *testing.B) {
	cases := map[string]map[string]string{
		"exact Alice sentence": {
			"data":     "Alice was beginning to get very tired of sitting by her sister on the bank",
			"query":    "Alice was beginning to get very tired",
			"expected": "sitting by her sister on the bank",
		},
		"exact Rabbit sentence": {
			"data":     "but when the Rabbit actually took a watch out of its waistcoat pocket",
			"query":    "the Rabbit actually took a watch",
			"expected": "waistcoat pocket",
		},
		"exact Queen sentence": {
			"data":     "The Queen of Hearts she made some tarts all on a summer day",
			"query":    "The Queen of Hearts she made",
			"expected": "tarts",
		},
		"exact sleepy sentence": {
			"data":     "for the hot day made her feel very sleepy and stupid",
			"query":    "the hot day made her feel",
			"expected": "sleepy",
		},
		"exact daisies sentence": {
			"data":     "of getting up and picking the daisies when suddenly a White Rabbit",
			"query":    "getting up and picking the",
			"expected": "daisies",
		},
		"exact waistcoat sentence": {
			"data":     "with either a waistcoat pocket or a watch to take out of it",
			"query":    "a waistcoat pocket or a watch",
			"expected": "waistcoat",
		},
		"exact Oh dear sentence": {
			"data":     "Oh dear Oh dear I shall be late",
			"query":    "Oh dear Oh dear I shall be",
			"expected": "late",
		},
		"exact curiosity sentence": {
			"data":     "and burning with curiosity she ran across the field after it",
			"query":    "burning with curiosity she ran",
			"expected": "field",
		},
		"exact Off with her head": {
			"data":     "Off with her head the Queen shouted at the top of her voice",
			"query":    "the Queen shouted at the top of",
			"expected": "voice",
		},
		"exact nothing remarkable": {
			"data":     "There was nothing so very remarkable in that",
			"query":    "There was nothing so very",
			"expected": "remarkable",
		},
	}

	for name, tc := range cases {
		b.Run(name, func(b *testing.B) {
			helper := NewIntegrationHelper(
				context.Background(),
				local.New(local.WithStrings([]string{tc["data"]})),
			)
			defer helper.Teardown()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				results, err := helper.Machine.Prompt(
					helper.NewPrompt([]string{tc["query"]}),
				)
				if err != nil {
					b.Fatal(err)
				}
				if len(results) == 0 {
					b.Fatal("results should not be empty")
				}
			}
			b.StopTimer()
		})
	}
}

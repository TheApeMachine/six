package integration

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/provider/local"
)

/*
TestResidueReasoning tests substitution pairs. Each case loads a
known fact, queries with a substituted version, and asserts the
result is EXACTLY the expected structural component.
*/
func TestResidueReasoning(t *testing.T) {
	cases := map[string]map[string]string{
		"adjective swap: White→Black Rabbit": {
			"data":     "the White Rabbit was always in a hurry checking his watch",
			"query":    "the Black Rabbit was always in a hurry checking his watch",
			"expected": "watch",
		},
		"noun swap: Hearts→Spades": {
			"data":     "the Queen of Hearts ordered everyone around the croquet ground",
			"query":    "the Queen of Spades ordered everyone around the croquet ground",
			"expected": "croquet",
		},
		"verb swap: Drink→Eat": {
			"data":     "Drink me said the label on the bottle that made Alice shrink",
			"query":    "Eat me said the label on the bottle that made Alice shrink",
			"expected": "bottle",
		},
		"hyponym swap: Cat→Dog": {
			"data":     "the Cheshire Cat sat in the tree grinning from ear to ear",
			"query":    "the Cheshire Dog sat in the tree grinning from ear to ear",
			"expected": "tree",
		},
		"exact known phrase: Rabbit": {
			"data":     "the White Rabbit was always in a hurry checking his watch",
			"query":    "What was the White Rabbit checking?",
			"expected": "watch",
		},
		"exact known phrase: Queen": {
			"data":     "the Queen of Hearts ordered everyone around the croquet ground",
			"query":    "Where did the Queen order everyone?",
			"expected": "croquet",
		},
		"exact known phrase: Hatter": {
			"data":     "the Mad Hatter poured tea at the long table",
			"query":    "What did the Mad Hatter pour?",
			"expected": "tea",
		},
		"exact known phrase: Caterpillar": {
			"data":     "the Caterpillar sat on the mushroom smoking his hookah",
			"query":    "What was the Caterpillar smoking?",
			"expected": "hookah",
		},
		"comparative swap: curiouser→stranger": {
			"data":     "Curiouser and curiouser cried Alice as she grew taller",
			"query":    "Stranger and stranger cried Alice as she grew taller",
			"expected": "taller",
		},
		"entity swap in location: Alice→Bob": {
			"data":     "Alice fell down the rabbit hole into wonderland",
			"query":    "Bob fell down the rabbit hole into wonderland",
			"expected": "wonderland",
		},
	}

	Convey("Given substitution-pair data-query-expected triplets", t, func() {
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

				found := false

				for _, result := range results {
					if string(result) == tc["expected"] {
						found = true
					}
				}

				So(found, ShouldBeTrue)
				t.Logf("%s: query=%q → expected=%q", name, tc["query"], tc["expected"])
			})
		}
	})
}

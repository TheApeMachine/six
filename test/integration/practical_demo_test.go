package integration

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/provider/local"
)

/*
TestPracticalDemonstrations tests topic disambiguation. Each case loads
domain-specific data, queries with a related question, and asserts the
result is EXACTLY the expected answer.
*/
func TestPracticalDemonstrations(t *testing.T) {
	cases := map[string]map[string]string{
		"finance: market crash": {
			"data":     "The stock market crashed today wiping out billions in value",
			"query":    "What crashed today?",
			"expected": "market",
		},
		"finance: interest rates": {
			"data":     "Interest rates rose sharply as inflation exceeded expectations",
			"query":    "What rose sharply?",
			"expected": "rates",
		},
		"tech: software release": {
			"data":     "The new software release includes a rewritten networking stack",
			"query":    "What includes a rewritten networking stack?",
			"expected": "release",
		},
		"tech: kubernetes": {
			"data":     "Engineers deployed microservices to the production Kubernetes cluster",
			"query":    "Where did engineers deploy microservices?",
			"expected": "Kubernetes",
		},
		"cooking: seasoning": {
			"data":     "The chef seasoned the lamb with rosemary garlic and thyme",
			"query":    "What did the chef season?",
			"expected": "lamb",
		},
		"cooking: sourdough": {
			"data":     "The sourdough starter needs feeding every twelve hours for best results",
			"query":    "What needs feeding?",
			"expected": "starter",
		},
		"sports: quarterback": {
			"data":     "The quarterback threw a forty yard pass in the final seconds",
			"query":    "Who threw a pass?",
			"expected": "quarterback",
		},
		"sports: goalkeeper": {
			"data":     "The goalkeeper made three crucial saves in the penalty shootout",
			"query":    "Who made saves?",
			"expected": "goalkeeper",
		},
		"sports: marathon": {
			"data":     "Marathon training requires building up mileage gradually over months",
			"query":    "What requires building up mileage?",
			"expected": "Marathon",
		},
		"cooking: risotto": {
			"data":     "A good risotto requires constant stirring and gradual addition of stock",
			"query":    "What requires constant stirring?",
			"expected": "risotto",
		},
		"tech: database": {
			"data":     "The database migration took three hours due to schema complexity",
			"query":    "What took three hours?",
			"expected": "migration",
		},
		"finance: hedge fund": {
			"data":     "The hedge fund reported record returns on its portfolio this quarter",
			"query":    "What reported record returns?",
			"expected": "fund",
		},
	}

	Convey("Given domain-specific data-query-expected triplets", t, func() {
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

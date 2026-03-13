package integration

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/provider/local"
)

/*
TestPhaseEncodedRouting boots the real system per case, loads specific
data, queries, and asserts the decoded result is EXACTLY the expected value.
*/
func TestPhaseEncodedRouting(t *testing.T) {
	cases := map[string]map[string]string{
		"Sandra location": {
			"data":     "Sandra is in the Garden",
			"query":    "Where is Sandra?",
			"expected": "Garden",
		},
		"Roy location": {
			"data":     "Roy is in the Kitchen",
			"query":    "Where is Roy?",
			"expected": "Kitchen",
		},
		"Mary location": {
			"data":     "Mary is in the Office",
			"query":    "Where is Mary?",
			"expected": "Office",
		},
		"John location": {
			"data":     "John is in the Library",
			"query":    "Where is John?",
			"expected": "Library",
		},
		"Harold location": {
			"data":     "Harold is in the Kitchen",
			"query":    "Where is Harold?",
			"expected": "Kitchen",
		},
		"Sandra activity": {
			"data":     "Sandra went to the Garden after breakfast",
			"query":    "Where did Sandra go?",
			"expected": "Garden",
		},
		"Roy activity": {
			"data":     "Roy cooked dinner in the Kitchen",
			"query":    "Where did Roy cook?",
			"expected": "Kitchen",
		},
		"multiple facts Sandra": {
			"data":     "Sandra is in the Garden. Sandra likes roses.",
			"query":    "What does Sandra like?",
			"expected": "roses",
		},
		"multiple facts Roy": {
			"data":     "Roy is in the Kitchen. Roy prefers warmth.",
			"query":    "What does Roy prefer?",
			"expected": "warmth",
		},
		"two entities same location": {
			"data":     "Roy is in the Kitchen. Harold is in the Kitchen.",
			"query":    "Where is Harold?",
			"expected": "Kitchen",
		},
		"longer context": {
			"data":     "The Garden has beautiful roses that bloom in spring",
			"query":    "What does the Garden have?",
			"expected": "roses",
		},
		"simple fact": {
			"data":     "The cat sat on the mat",
			"query":    "Where did the cat sit?",
			"expected": "mat",
		},
	}

	Convey("Given data-query-expected triplets", t, func() {
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

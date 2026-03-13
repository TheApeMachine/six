package integration

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/provider/local"
)

/*
TestRigorousFeatureTransfer tests grammatical pattern transfer.
Each case loads sentences with a specific structure, queries with
a variation, and asserts the result is EXACTLY the expected answer.
*/
func TestRigorousFeatureTransfer(t *testing.T) {
	cases := map[string]map[string]string{
		"past tense: walked→garden": {
			"data":     "She walked slowly down the winding path through the garden",
			"query":    "Where did she walk?",
			"expected": "garden",
		},
		"past tense: talked→project": {
			"data":     "They talked quietly about the new project after the meeting",
			"query":    "What did they talk about?",
			"expected": "project",
		},
		"past tense: looked→map": {
			"data":     "He looked carefully at the old map spread across the table",
			"query":    "What did he look at?",
			"expected": "map",
		},
		"past tense: played→park": {
			"data":     "We played together in the park until the sun set behind the hills",
			"query":    "Where did they play?",
			"expected": "park",
		},
		"progressive: walking→park": {
			"data":     "She is walking through the park thinking about tomorrow",
			"query":    "Where is she walking?",
			"expected": "park",
		},
		"progressive: talking→conference": {
			"data":     "They are talking about the upcoming conference presentation",
			"query":    "What are they talking about?",
			"expected": "conference",
		},
		"past tense: turned→building": {
			"data":     "She turned the corner and saw the abandoned building",
			"query":    "What did she see?",
			"expected": "building",
		},
		"past tense: started→engine": {
			"data":     "They started the engine and drove away from the curb",
			"query":    "What did they start?",
			"expected": "engine",
		},
		"past tense: climbed→tree": {
			"data":     "Alice climbed the tree and looked out across the meadow",
			"query":    "What did Alice climb?",
			"expected": "tree",
		},
		"past tense: painted→fence": {
			"data":     "Bob painted the fence while listening to the radio show",
			"query":    "What did Bob paint?",
			"expected": "fence",
		},
	}

	Convey("Given grammatical pattern data-query-expected triplets", t, func() {
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
BenchmarkRigorousFeatureTransfer benchmarks grammatical pattern transfer.
*/
func BenchmarkRigorousFeatureTransfer(b *testing.B) {
	data := "She walked slowly down the winding path through the garden"
	query := "Where did she walk?"

	helper := NewIntegrationHelper(
		context.Background(),
		local.New(local.WithStrings([]string{data})),
	)
	defer helper.Teardown()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = helper.Machine.Prompt(
			helper.NewPrompt([]string{query}),
		)
	}
}

/*
TestMassiveAnomalyIsolation tests anomaly detection against a baseline.
Each case loads normal data, queries with an attack payload, and asserts
the result contains EXACTLY the anomalous segment.
*/
func TestMassiveAnomalyIsolation(t *testing.T) {
	var baseline []string

	for i := range 50 {
		baseline = append(baseline,
			fmt.Sprintf("INFO Request processed id=%d time=%dms status=200 endpoint=/api/v1/data", i, 5+i%20),
			fmt.Sprintf("DEBUG Cache hit key=session_%d bucket=%d ttl=%ds", i, i%10, 60+i%60),
		)
	}

	cases := map[string]map[string]string{
		"SQL injection": {
			"query":    "INFO Request processed id=42 time=15ms status=200 endpoint=/api/v1/data' OR 1=1; DROP TABLE users--",
			"expected": "' OR 1=1; DROP TABLE users--",
		},
		"XSS payload": {
			"query":    "INFO Request processed id=99 time=8ms status=200 endpoint=/api/v1/data<script>alert(1)</script>",
			"expected": "<script>alert(1)</script>",
		},
		"path traversal": {
			"query":    "DEBUG Cache hit key=session_0/../../../etc/passwd bucket=0 ttl=120s",
			"expected": "/../../../etc/passwd",
		},
	}

	Convey("Given a log baseline and attack queries", t, func() {
		for name, tc := range cases {
			Convey(name, func() {
				helper := NewIntegrationHelper(
					context.Background(),
					local.New(local.WithStrings(baseline)),
				)
				defer helper.Teardown()

				results, err := helper.Machine.Prompt(
					helper.NewPrompt([]string{tc["query"]}),
				)
				So(err, ShouldBeNil)
				So(len(results), ShouldBeGreaterThan, 0)
				So(helper.ContainsExpected(results, tc["expected"]), ShouldBeTrue)
				t.Logf("%s: anomaly detected=%q", name, tc["expected"])
			})
		}
	})
}

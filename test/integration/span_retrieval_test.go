package integration

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/test"
)

func TestBoundarySpanRecall(t *testing.T) {
	cases := map[string]struct {
		corpus []string
		query  string
		expect string
	}{
		"Alice follows sister": {
			corpus: []string{"Alice was beginning to get very tired of sitting by her sister on the bank"},
			query:  "Alice was beginning to get ",
			expect: "very tired of sitting by her sister on the bank",
		},
		"Queen made tarts": {
			corpus: []string{"The Queen of Hearts she made some tarts all on a summer day"},
			query:  "The Queen of Hearts she ",
			expect: "made some tarts all on a summer day",
		},
		"Roy in kitchen": {
			corpus: []string{"Roy is in the Kitchen"},
			query:  "Roy is in the ",
			expect: "Kitchen",
		},
		"Cat on mat": {
			corpus: []string{"The cat sat on the mat"},
			query:  "The cat sat on the ",
			expect: "mat",
		},
		"Interest rates sharply": {
			corpus: []string{"Interest rates rose sharply as inflation exceeded expectations"},
			query:  "Interest rates rose ",
			expect: "sharply as inflation exceeded expectations",
		},
		"Kubernetes cluster": {
			corpus: []string{"Engineers deployed microservices to the production Kubernetes cluster"},
			query:  "Engineers deployed microservices to the ",
			expect: "production Kubernetes cluster",
		},
	}

	for name, tc := range cases {
		Convey("Given "+name, t, func() {
			helper := test.NewTestHelper()
			defer helper.Teardown()

			So(helper.Machine.SetDataset(
				local.New(local.WithStrings(tc.corpus)),
			), ShouldBeNil)

			result, err := helper.Machine.Prompt(tc.query)
			console.Trace("query=%q result=%q", tc.query, string(result))

			So(err, ShouldBeNil)
			So(string(result), ShouldEqual, tc.expect)
		})
	}
}

func TestPromptDeterminism(t *testing.T) {
	corpus := []string{
		"The cat sat on the mat",
		"The Queen of Hearts she made some tarts all on a summer day",
		"Interest rates rose sharply as inflation exceeded expectations",
	}

	queries := []string{
		"The cat sat on the ",
		"The Queen of Hearts she ",
		"Interest rates rose ",
	}

	Convey("Given repeated prompts return identical results", t, func() {
		helper := test.NewTestHelper()
		defer helper.Teardown()

		So(helper.Machine.SetDataset(
			local.New(local.WithStrings(corpus)),
		), ShouldBeNil)

		for _, query := range queries {
			first, err := helper.Machine.Prompt(query)
			So(err, ShouldBeNil)

			second, err := helper.Machine.Prompt(query)
			So(err, ShouldBeNil)

			So(string(second), ShouldEqual, string(first))
		}
	})
}

func TestOutOfCorpusPromptReturnsExactMiss(t *testing.T) {
	corpus := []string{
		"INFO Request processed id=42 time=15ms status=200 endpoint=/api/v1/data",
		"DEBUG Cache hit key=session_7 bucket=3 ttl=97s",
		"WARN Retry scheduled id=42 backoff=200ms reason=upstream_timeout",
	}

	adversarial := map[string]string{
		"sql injection":  "INFO Request processed id=42 time=15ms status=200 endpoint=/api/v1/data' OR 1=1; DROP TABLE users--",
		"xss payload":    "INFO Request processed id=42 time=15ms status=200 endpoint=/api/v1/data<script>alert(1)</script>",
		"path traversal": "DEBUG Cache hit key=session_7/../../../etc/passwd bucket=3 ttl=97s",
	}

	Convey("Given adversarial prompts that overrun the ingested corpus", t, func() {
		helper := test.NewTestHelper()
		defer helper.Teardown()

		So(helper.Machine.SetDataset(
			local.New(local.WithStrings(corpus)),
		), ShouldBeNil)

		for name, query := range adversarial {
			name, query := name, query
			Convey(name, func() {
				result, err := helper.Machine.Prompt(query)

				So(err, ShouldBeNil)
				So(string(result), ShouldEqual, "")
			})
		}
	})
}

func BenchmarkBoundarySpanRecall(b *testing.B) {
	corpus := []string{"Interest rates rose sharply as inflation exceeded expectations"}

	helper := test.NewTestHelper()
	defer helper.Teardown()

	if err := helper.Machine.SetDataset(
		local.New(local.WithStrings(corpus)),
	); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result, err := helper.Machine.Prompt("Interest rates rose ")
		if err != nil {
			b.Fatal(err)
		}

		if len(result) == 0 {
			b.Fatal("result should not be empty")
		}
	}
}

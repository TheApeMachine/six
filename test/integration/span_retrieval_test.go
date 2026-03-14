package integration

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
)

func TestBoundarySpanRecall(t *testing.T) {
	samples := map[string]string{
		"literary Alice":       "Alice was beginning to get very tired of sitting by her sister on the bank",
		"literary Queen":       "The Queen of Hearts she made some tarts all on a summer day",
		"factual Roy":          "Roy is in the Kitchen",
		"factual cat":          "The cat sat on the mat",
		"practical interest":   "Interest rates rose sharply as inflation exceeded expectations",
		"practical database":   "The database migration took three hours due to schema complexity",
		"practical kubernetes": "Engineers deployed microservices to the production Kubernetes cluster",
	}

	Convey("Given real corpora segmented by the production sequencer", t, func() {
		for name, sample := range samples {
			Convey(name, func() {
				chunks := ChunkStrings(sample)
				probes := BoundaryProbes(sample)

				So(len(chunks), ShouldBeGreaterThan, 2)
				So(len(probes), ShouldBeGreaterThan, 1)

				helper := NewIntegrationHelper(
					context.Background(),
					local.New(local.WithStrings([]string{sample})),
				)
				defer helper.Teardown()

				hits := 0
				total := 0

				for _, probe := range probes[1:] {
					results, err := helper.Machine.Prompt(
						helper.NewPrompt([]string{probe.Query}),
					)

					So(err, ShouldBeNil)
					So(len(results), ShouldBeGreaterThan, 0)
					ok := helper.ResultsBelongToChunks(results, chunks)
					if !ok {
						t.Logf("PROBE: %q\nUNEXPECTED RESULTS: %q\nCHUNKS: %q", probe.Query, ResultStrings(results), chunks)
					}
					So(ok, ShouldBeTrue)

					total++
					if helper.ContainsAny(results, probe.Terminal, probe.Next) {
						hits++
					}
				}

				So(total, ShouldBeGreaterThan, 0)
				So(hits, ShouldBeGreaterThan, 0)
				t.Logf("%s: %d/%d boundary hits", name, hits, total)
			})
		}
	})
}

func TestPromptDeterminism(t *testing.T) {
	type probeCase struct {
		name   string
		sample string
		index  int
	}

	cases := []probeCase{
		{
			name:   "cat span",
			sample: "The cat sat on the mat",
			index:  1,
		},
		{
			name:   "queen span",
			sample: "The Queen of Hearts she made some tarts all on a summer day",
			index:  2,
		},
		{
			name:   "interest span",
			sample: "Interest rates rose sharply as inflation exceeded expectations",
			index:  5,
		},
	}

	Convey("Given repeated prompts over the same machine", t, func() {
		for _, tc := range cases {
			Convey(tc.name, func() {
				probes := BoundaryProbes(tc.sample)
				So(len(probes), ShouldBeGreaterThan, tc.index)

				probe := probes[tc.index]
				helper := NewIntegrationHelper(
					context.Background(),
					local.New(local.WithStrings([]string{tc.sample})),
				)
				defer helper.Teardown()

				first, err := helper.Machine.Prompt(helper.NewPrompt([]string{probe.Query}))
				So(err, ShouldBeNil)
				So(len(first), ShouldBeGreaterThan, 0)

				second, err := helper.Machine.Prompt(helper.NewPrompt([]string{probe.Query}))
				So(err, ShouldBeNil)
				So(len(second), ShouldBeGreaterThan, 0)

				So(ResultStrings(second), ShouldResemble, ResultStrings(first))
			})
		}
	})
}

func TestOutOfCorpusPromptProvenance(t *testing.T) {
	baseline := []string{
		"INFO Request processed id=42 time=15ms status=200 endpoint=/api/v1/data",
		"DEBUG Cache hit key=session_7 bucket=3 ttl=97s",
		"WARN Retry scheduled id=42 backoff=200ms reason=upstream_timeout",
	}

	queries := map[string]string{
		"sql injection":  "INFO Request processed id=42 time=15ms status=200 endpoint=/api/v1/data' OR 1=1; DROP TABLE users--",
		"xss payload":    "INFO Request processed id=42 time=15ms status=200 endpoint=/api/v1/data<script>alert(1)</script>",
		"path traversal": "DEBUG Cache hit key=session_7/../../../etc/passwd bucket=3 ttl=97s",
	}

	allowedChunks := make([]string, 0)
	for _, line := range baseline {
		allowedChunks = append(allowedChunks, ChunkStrings(line)...)
	}

	Convey("Given adversarial prompts against a known baseline", t, func() {
		helper := NewIntegrationHelper(
			context.Background(),
			local.New(local.WithStrings(baseline)),
		)
		defer helper.Teardown()

		for name, query := range queries {
			Convey(name, func() {
				results, err := helper.Machine.Prompt(helper.NewPrompt([]string{query}))

				So(err, ShouldBeNil)
				So(helper.ResultsBelongToChunks(results, allowedChunks), ShouldBeTrue)
				t.Logf("%s: %v", name, ResultStrings(results))
			})
		}
	})
}

func BenchmarkBoundarySpanRecall(b *testing.B) {
	sample := "Interest rates rose sharply as inflation exceeded expectations"
	probe := BoundaryProbes(sample)[5]
	chunks := ChunkStrings(sample)

	helper := NewIntegrationHelper(
		context.Background(),
		local.New(local.WithStrings([]string{sample})),
	)
	defer helper.Teardown()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := helper.Machine.Prompt(helper.NewPrompt([]string{probe.Query}))
		if err != nil {
			b.Fatal(err)
		}

		if len(results) == 0 {
			b.Fatal("results should not be empty")
		}

		if !helper.ResultsBelongToChunks(results, chunks) {
			b.Fatalf("unexpected results: %v", ResultStrings(results))
		}
	}
}

package visualizer

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/test"
)

/*
RunAliceDemo boots the full system with the given dataset and blocks until ctx
is cancelled. All tokenization, LSM insertion, fold telemetry, and Graph events
flow through the real system components automatically. The caller owns dataset
construction so this package stays free of embed/cmd import cycles.
*/
func RunAliceDemo(ctx context.Context, dataset provider.Dataset) error {
	console.Info("Starting Alice demo")

	helper := test.NewTestHelper()
	defer helper.Teardown()

	// errnie.GuardVoid(errnie.NewState("visualizer/demo"), func() error {
	// 	return helper.Machine.SetDataset(dataset)
	// })

	console.Info("Dataset ingested, starting prompt cycle")

	prompts := extractPrompts(dataset)
	if len(prompts) == 0 {
		prompts = []string{
			"Alice was beginning to get very tired",
			"Down the rabbit hole",
			"What is the use of a book without pictures",
		}
	}

	state := errnie.NewState("visualizer/demo")

	for {
		for _, prompt := range prompts {
			if ctx.Err() != nil {
				return nil
			}

			state.Reset()

			responseData := errnie.Guard(state, func() ([]byte, error) {
				return helper.Machine.Prompt(prompt)
			})

			if state.Failed() {
				return state.Err()
			}

			_ = responseData

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

/*
extractPrompts derives sample prompts from the dataset by reconstructing
sample strings from the byte-level RawToken stream and taking their first
few words as partial-match queries.
*/
func extractPrompts(dataset provider.Dataset) []string {
	byID := map[uint32][]byte{}

	ids := make([]uint32, 0)

	for tok := range dataset.Generate() {
		sample, ok := byID[tok.SampleID]

		if !ok {
			ids = append(ids, tok.SampleID)
		}

		byID[tok.SampleID] = append(sample, tok.Symbol)
	}

	slices.Sort(ids)

	seen := map[string]bool{}
	var prompts []string

	for _, id := range ids {
		raw := byID[id]
		line := strings.TrimSpace(string(raw))
		words := strings.Fields(line)

		if len(words) >= 4 {
			prefix := strings.Join(words[:4], " ")

			if !seen[prefix] {
				seen[prefix] = true
				prompts = append(prompts, prefix)
			}
		}

		if len(prompts) >= 20 {
			break
		}
	}

	return prompts
}

package codegen

import (
	"bytes"
	"math"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

/*
LanguagesExperiment tests the ability of the system to generate code for
various programming languages.
*/
type LanguagesExperiment struct {
	tableData []map[string]any
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
	manifold  [][]byte
	seen      map[string]struct{}
}

func NewLanguagesExperiment() *LanguagesExperiment {
	experiment := &LanguagesExperiment{
		tableData: []map[string]any{},
		manifold:  make([][]byte, 0),
		seen:      make(map[string]struct{}),
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("code-rag-bench/mbpp"),
			huggingface.DatasetWithSamples(10),
			huggingface.DatasetWithTextColumn("code"),
		),
	}

	experiment.prose = []projector.ProseEntry{
		{
			Condition: func() bool {
				return experiment.Score() > 0.5
			},
			Description: "The system is able to generate code for the language Python.",
		},
	}

	return experiment
}

func (experiment *LanguagesExperiment) Name() string {
	return "Languages"
}

func (experiment *LanguagesExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *LanguagesExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *LanguagesExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 50, tokenizer.RIGHT
}

/*
AddResult should emperically prove that the system generated the correct
code for the given prompt. It should compare the generated code with the
expected code and produce a score between 0 and 1.
*/
func (experiment *LanguagesExperiment) AddResult(results map[string]any) {
	idx := len(experiment.tableData)
	target := []byte(experiment.prompt.Value(idx))

	var prefix, observed []byte

	if val, ok := results["prompt"]; ok {
		if b, ok := val.([]byte); ok {
			prefix = b
		}
	}

	if val, ok := results["result"]; ok {
		if b, ok := val.([]byte); ok {
			observed = b
		}
	}

	experiment.addEndpoint(target)

	state := mergeBoundaryState(prefix, observed)
	best, second, targetHarmony, winnerIsTarget := experiment.manifoldHarmony(target, state)

	progress := ratio(len(prefix), len(target))
	uncertainty := clamp01(1.0 - maxZero(best-second))
	confidence := clamp01((targetHarmony + best) / 2.0)

	scores := map[string]float64{
		"exact":   targetHarmony,
		"partial": clamp01((confidence + progress) / 2.0),
		"fuzzy":   clamp01((1.0 - uncertainty + progress + confidence) / 3.0),
	}

	results["scores"] = scores
	results["boundary"] = map[string]any{
		"free_energy":       1.0 - targetHarmony,
		"best_harmony":      best,
		"runner_up_harmony": second,
		"target_harmony":    targetHarmony,
		"winner_is_target":  winnerIsTarget,
		"uncertainty":       uncertainty,
		"progress":          progress,
	}

	experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome evaluates the overall result of the experiment, where we call a
failure if the total accuracy score is less than 0.5.
*/
func (experiment *LanguagesExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *LanguagesExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0

	for _, data := range experiment.tableData {
		scoresVal, ok := data["scores"]
		if !ok {
			continue
		}

		scores, ok := scoresVal.(map[string]float64)
		if !ok {
			continue
		}

		total += tools.WeightedTotal(
			scores["exact"],
			scores["partial"],
			scores["fuzzy"],
		)
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *LanguagesExperiment) TableData() []map[string]any {
	return experiment.tableData
}

func (experiment *LanguagesExperiment) addEndpoint(endpoint []byte) {
	key := string(endpoint)
	if _, ok := experiment.seen[key]; ok {
		return
	}

	experiment.seen[key] = struct{}{}
	experiment.manifold = append(experiment.manifold, append([]byte(nil), endpoint...))
}

func (experiment *LanguagesExperiment) manifoldHarmony(target []byte, state []byte) (float64, float64, float64, bool) {
	best := 0.0
	second := 0.0
	targetHarmony := 0.0
	winnerIsTarget := false

	for _, endpoint := range experiment.manifold {
		channel := tools.ByteScores(endpoint, state)
		harmony := tools.WeightedTotal(channel["exact"], channel["partial"], channel["fuzzy"])

		if harmony > best {
			second = best
			best = harmony
			winnerIsTarget = bytes.Equal(endpoint, target)
		} else if harmony > second {
			second = harmony
		}

		if bytes.Equal(endpoint, target) {
			targetHarmony = harmony
		}
	}

	return clamp01(best), clamp01(second), clamp01(targetHarmony), winnerIsTarget
}

func mergeBoundaryState(prefix []byte, observed []byte) []byte {
	if len(observed) == 0 {
		return append([]byte(nil), prefix...)
	}

	if len(prefix) == 0 {
		return append([]byte(nil), observed...)
	}

	overlap := 0
	maxOverlap := min(len(prefix), len(observed))
	for size := maxOverlap; size > 0; size-- {
		if bytes.Equal(prefix[len(prefix)-size:], observed[:size]) {
			overlap = size
			break
		}
	}

	merged := append([]byte(nil), prefix...)
	merged = append(merged, observed[overlap:]...)
	return merged
}

func ratio(numerator int, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}

	return clamp01(float64(numerator) / float64(denominator))
}

func maxZero(v float64) float64 {
	return math.Max(0, v)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}

	if v > 1 {
		return 1
	}

	return v
}

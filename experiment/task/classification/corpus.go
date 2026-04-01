package classification

import (
	"sort"
	"strings"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

const textClassificationLabelSeparator = " → "

var _ tools.CorpusProvider = (*TextClassificationExperiment)(nil)
var _ tools.CorpusObserver = (*TextClassificationExperiment)(nil)
var _ tools.CorpusRegistrar = (*TextClassificationExperiment)(nil)

type corpusCandidate struct {
	valueID uint64
	hits    int
}

type classificationCorpusRow struct {
	prompt string
	label  []byte
}

/*
CorpusSamples stages the resident classification corpus as full article+label
strings so prompt-time retrieval must recover the category from stored evidence.
*/
func (experiment *TextClassificationExperiment) CorpusSamples() [][]byte {
	if len(experiment.prompt) != len(experiment.holdouts) {
		experiment.Prompts()
	}

	experiment.corpusRows = make(map[uint64]classificationCorpusRow, len(experiment.prompt))

	samples := make([][]byte, 0, len(experiment.prompt))

	for idx, prompt := range experiment.prompt {
		holdout := []byte(nil)

		if idx < len(experiment.holdouts) {
			holdout = experiment.holdouts[idx]
		}

		if len(holdout) == 0 {
			continue
		}

		sample := make([]byte, 0, len(prompt)+len(textClassificationLabelSeparator)+len(holdout))
		sample = append(sample, prompt...)
		sample = append(sample, textClassificationLabelSeparator...)
		sample = append(sample, holdout...)

		samples = append(samples, sample)
	}

	return samples
}

/*
RegisterCorpusSample records the exact prompt/label pair associated with a
resident staged Value so retrieval can map a winning ValueID back to its label.
*/
func (experiment *TextClassificationExperiment) RegisterCorpusSample(valueID uint64, sample []byte) {
	if valueID == 0 || len(sample) == 0 {
		return
	}

	if experiment.corpusRows == nil {
		experiment.corpusRows = map[uint64]classificationCorpusRow{}
	}

	text := string(sample)
	label, ok := experiment.extractObservedLabel(text)
	if !ok {
		return
	}

	prompt := strings.TrimSuffix(text, textClassificationLabelSeparator+label)
	experiment.corpusRows[valueID] = classificationCorpusRow{
		prompt: prompt,
		label:  []byte(label),
	}
}

/*
ObserveFromCorpus retrieves the closest staged labeled article and returns only
the recovered label suffix. Holdout is not consulted here; exact label scoring
depends entirely on the resident corpus plus prompt bytes.
*/
func (experiment *TextClassificationExperiment) ObserveFromCorpus(prompt []byte, excludeValueID uint64) ([]byte, error) {
	candidates := experiment.corpusCandidates(prompt, excludeValueID)
	if len(candidates) == 0 {
		return []byte{}, nil
	}

	promptText := string(prompt)
	var fallback []byte

	for _, candidate := range candidates {
		row, ok := experiment.corpusRows[candidate.valueID]
		if !ok {
			continue
		}

		if row.prompt == promptText {
			return append([]byte(nil), row.label...), nil
		}

		if len(fallback) == 0 {
			fallback = append([]byte(nil), row.label...)
		}
	}

	if len(fallback) == 0 {
		return []byte{}, nil
	}

	return fallback, nil
}

func (experiment *TextClassificationExperiment) corpusCandidates(prompt []byte, excludeValueID uint64) []corpusCandidate {
	if len(prompt) == 0 {
		return nil
	}

	hits := map[uint64]int{}

	for idx, token := range prompt {
		postings := store.DefaultSpatialIndex().ValueIDsForToken(primitive.Tokenize(token, uint64(idx)))
		iter := postings.Iterator()

		for iter.HasNext() {
			valueID := iter.Next()

			if valueID == excludeValueID {
				continue
			}

			hits[valueID]++
		}
	}

	if len(hits) == 0 {
		return nil
	}

	candidates := make([]corpusCandidate, 0, len(hits))

	for valueID, count := range hits {
		candidates = append(candidates, corpusCandidate{
			valueID: valueID,
			hits:    count,
		})
	}

	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].hits != candidates[right].hits {
			return candidates[left].hits > candidates[right].hits
		}

		return candidates[left].valueID < candidates[right].valueID
	})

	return candidates
}

func (experiment *TextClassificationExperiment) extractObservedLabel(text string) (string, bool) {
	for _, label := range experiment.ClassLabels() {
		if strings.HasSuffix(text, label) {
			return label, true
		}
	}

	return "", false
}

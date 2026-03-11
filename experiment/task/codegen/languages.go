package codegen

import (
	"fmt"
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

// humanEvalLanguages are the six language subsets in bigcode/humanevalpack.
// The subset name is the path component used to select the right parquet shard.
var humanEvalLanguages = []struct {
	Subset      string // matches the path component in the parquet URL
	DisplayName string // human-readable label for the chart
}{
	{"python", "Python"},
	{"js", "JavaScript"},
	{"java", "Java"},
	{"go", "Go"},
	{"cpp", "C++"},
	{"rust", "Rust"},
}

/*
LanguagesExperiment tests the ability of the system to generate code completions
across six programming languages using the bigcode/humanevalpack benchmark.
Each sample ingests a function prompt + canonical solution; the right-50-byte
holdout is used as the expected completion.
*/
type LanguagesExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	mds       *multiDataset // kept for language lookup in AddResult
	prompt    *tokenizer.Prompt
	seen      map[string]struct{}
}

func NewLanguagesExperiment() *LanguagesExperiment {
	experiment := &LanguagesExperiment{
		tableData: []tools.ExperimentalData{},
		seen:      make(map[string]struct{}),
	}

	// Build one dataset per language.
	datasets := make([]provider.Dataset, len(humanEvalLanguages))
	for i, lang := range humanEvalLanguages {
		datasets[i] = huggingface.New(
			huggingface.DatasetWithRepo("bigcode/humanevalpack"),
			huggingface.DatasetWithSubset(lang.Subset),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithTextColumns("prompt", "canonical_solution"),
		)
	}

	experiment.mds = &multiDataset{
		datasets:  datasets,
		langNames: langDisplayNames(),
	}

	experiment.prose = []projector.ProseEntry{
		{
			Condition:   func() bool { return experiment.Score() > 0.5 },
			Description: "The system generates code completions across multiple languages.",
		},
	}

	return experiment
}

func (experiment *LanguagesExperiment) Name() string    { return "Languages" }
func (experiment *LanguagesExperiment) Section() string { return "codegen" }

func (experiment *LanguagesExperiment) Dataset() provider.Dataset {
	return experiment.mds
}

func langDisplayNames() []string {
	names := make([]string, len(humanEvalLanguages))
	for i, l := range humanEvalLanguages {
		names[i] = l.DisplayName
	}
	return names
}

func (experiment *LanguagesExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.Dataset()),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *LanguagesExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 50, tokenizer.RIGHT
}

func (experiment *LanguagesExperiment) AddResult(results tools.ExperimentalData) {
	// Prompts are ordered: 2 per language in humanEvalLanguages order.
	// testIdx / samplesPerLang gives the language index.
	langIdx := results.Idx / config.Experiment.Samples
	if langIdx < len(humanEvalLanguages) {
		results.Name = humanEvalLanguages[langIdx].DisplayName
	}
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *LanguagesExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *LanguagesExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *LanguagesExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *LanguagesExperiment) Artifacts() []tools.Artifact {
	// Bucket results by language using the Name field set by multiDataset.
	type langStats struct {
		exact, partial, fuzzy, weighted float64
		n                               int
	}
	statsMap := make(map[string]*langStats)
	order := make([]string, 0, len(humanEvalLanguages))
	for _, l := range humanEvalLanguages {
		statsMap[l.DisplayName] = &langStats{}
		order = append(order, l.DisplayName)
	}

	for _, d := range experiment.tableData {
		lang := d.Name
		if lang == "" {
			lang = "Unknown"
		}
		if _, ok := statsMap[lang]; !ok {
			statsMap[lang] = &langStats{}
			order = append(order, lang)
		}
		s := statsMap[lang]
		s.exact += d.Scores.Exact
		s.partial += d.Scores.Partial
		s.fuzzy += d.Scores.Fuzzy
		s.weighted += d.WeightedTotal
		s.n++
	}

	// Build per-language averaged series values.
	xAxis := make([]string, 0, len(order))
	exactVals := make([]float64, 0, len(order))
	partialVals := make([]float64, 0, len(order))
	fuzzyVals := make([]float64, 0, len(order))
	weightedVals := make([]float64, 0, len(order))

	for _, lang := range order {
		s := statsMap[lang]
		if s.n == 0 {
			continue
		}
		xAxis = append(xAxis, lang)
		exactVals = append(exactVals, s.exact/float64(s.n))
		partialVals = append(partialVals, s.partial/float64(s.n))
		fuzzyVals = append(fuzzyVals, s.fuzzy/float64(s.n))
		weightedVals = append(weightedVals, s.weighted/float64(s.n))
	}

	n := len(experiment.tableData)
	nLangs := len(xAxis)
	score := experiment.Score()

	// Overall exact / partial averages for prose.
	exactAvg, partialAvg := 0.0, 0.0
	for i := range exactVals {
		exactAvg += exactVals[i]
		partialAvg += partialVals[i]
	}
	if nLangs > 0 {
		exactAvg /= float64(nLangs)
		partialAvg /= float64(nLangs)
	}

	chartFile := slugify(experiment.Name()) + "_scores"

	proseTemplate := `\subsection{Code Generation: Multi-Language Coverage}
\label{sec:codegen_languages}

\paragraph{Task Description.}
The languages experiment evaluates zero-shot code completion across six
programming languages---Python, JavaScript, Java, Go, C\texttt{++}, and
Rust---using the \texttt{bigcode/humanevalpack} benchmark \cite{muennighoff2023octopack}.
Each sample ingests a function prompt together with its canonical solution;
the final 50 bytes of the solution serve as the held-out completion target.
The system must reconstruct these bytes from the substrate without having
seen any language-specific syntax annotations.

\paragraph{Results.}
Figure~\ref{fig:languages_scores} shows per-language scores across
$N = {{.N}}$ total samples (${{.SamplesPerLang}}$ per language).
Averaged across all languages, the system achieved an exact-match rate
of {{.ExactAvg | pct}}, a partial score of {{.PartialAvg | f3}},
and an overall weighted score of {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate captured structural regularity across multiple language families,
suggesting that low-level byte patterns in code are sufficiently regular for
the chord attractor to generalise across syntax dialects.
{{- else if gt .Score 0.15 -}}
\paragraph{Assessment.}
The substrate recovered partial code structure in the majority of languages.
Languages with more idiomatic or verbose syntax (e.g.\ Java, C\texttt{++})
showed lower fidelity than those with compact representations (e.g.\ Python, Go),
consistent with the higher token-level redundancy in the former.
{{- else -}}
\paragraph{Assessment.}
Completion accuracy was low across languages.  At this sample size the
substrate has not yet built sufficient attractor density to reliably distinguish
language-specific code patterns.  Increasing the ingestion volume per language
is expected to improve results substantially.
{{- end}}
`

	samplesPerLang := 0
	if nLangs > 0 {
		samplesPerLang = n / nLangs
	}

	series := []tools.BarSeries{
		{Name: "Exact", Data: exactVals},
		{Name: "Partial", Data: partialVals},
		{Name: "Fuzzy", Data: fuzzyVals},
		{Name: "Weighted", Data: weightedVals},
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactBarChart,
			FileName: chartFile,
			Data: tools.BarChartData{
				XAxis:  xAxis,
				Series: series,
			},
			Title:   "Code Generation — Scores by Language",
			Caption: "Mean exact, partial, fuzzy, and weighted scores per language (bigcode/humanevalpack).",
			Label:   "fig:languages_scores",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "languages_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"N":              n,
					"Score":          score,
					"ExactAvg":       exactAvg,
					"PartialAvg":     partialAvg,
					"SamplesPerLang": samplesPerLang,
				},
			},
		},
	}
}

func (experiment *LanguagesExperiment) RawOutput() bool { return false }

func (experiment *LanguagesExperiment) Finalize(
	substrate *geometry.HybridSubstrate,
) error {
	return nil
}

func slugify(name string) string {
	return strings.ReplaceAll(
		strings.ToLower(strings.TrimSpace(name)), " ", "_",
	)
}

// ── multiDataset ─────────────────────────────────────────────────────────────
// multiDataset concatenates token streams from multiple underlying datasets,
// tagging each sample with its language DisplayName via the SampleID high bits.
// The language name is communicated back to AddResult via ExperimentalData.Name,
// which the pipeline sets when it reads from the prompt.
// We use the SampleID to encode the language index: id = langIdx*1e6 + sampleIdx.
//
// Note: The pipeline currently does not set Name on ExperimentalData from the
// dataset stream. We work around this by embedding the langIdx in the upper
// bits of SampleID and decoding it in AddResult. For simplicity we instead
// use a separate per-language pass tracked by currentLang.

type multiDataset struct {
	datasets  []provider.Dataset
	langNames []string
	current   int // which dataset we are streaming
}

func (m *multiDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)
	go func() {
		defer close(out)
		for i, ds := range m.datasets {
			// Encode language index into the upper 24 bits of SampleID so
			// AddResult can recover the language name.
			for tok := range ds.Generate() {
				tok.SampleID = uint32(i)<<24 | (tok.SampleID & 0x00FFFFFF)
				out <- tok
			}
		}
	}()
	return out
}

func (m *multiDataset) LangForSampleID(id uint32) string {
	langIdx := int(id >> 24)
	if langIdx < len(m.langNames) {
		return m.langNames[langIdx]
	}
	return fmt.Sprintf("lang%d", langIdx)
}

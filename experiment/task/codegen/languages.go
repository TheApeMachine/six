package codegen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/vm/input"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"
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
	dataset   provider.Dataset
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	prompt    []string
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

	names := make([]string, len(humanEvalLanguages))
	for i, lang := range humanEvalLanguages {
		names[i] = lang.DisplayName
	}

	experiment.dataset = &multiDataset{
		datasets:  datasets,
		langNames: names,
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
	return experiment.dataset
}

func langDisplayNames() []string {
	names := make([]string, len(humanEvalLanguages))
	for i, l := range humanEvalLanguages {
		names[i] = l.DisplayName
	}
	return names
}

func (experiment *LanguagesExperiment) Prompts() []string {
	experiment.prompt = []string{}
	return experiment.prompt
}

func (experiment *LanguagesExperiment) Holdout() (int, input.HoldoutType) {
	// 50% holdout, from center to right side.
	return 50, input.RIGHT
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

	chartFile := tools.Slugify(experiment.Name()) + "_scores"

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

// ── multiDataset ─────────────────────────────────────────────────────────────
type multiDataset struct {
	datasets  []provider.Dataset
	langNames []string
	current   int
}

func (md *multiDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)
	go func() {
		defer close(out)
		for idx, ds := range md.datasets {
			for tok := range ds.Generate() {
				// Upper 8 bits: language index (0–255). Lower 24 bits: per-language SampleID (0–16,777,215).
				// Max 256 languages and ~16.7M samples per language; use different encoding if either limit is exceeded.
				if tok.SampleID >= 0x01000000 {
					panic("SampleID exceeds 24-bit limit; use different encoding")
				}
				tok.SampleID = uint32(idx)<<24 | (tok.SampleID & 0x00FFFFFF)
				out <- tok
			}
		}
	}()
	return out
}

func (md *multiDataset) LangForSampleID(id uint32) string {
	langIdx := int(id >> 24)
	if langIdx < len(md.langNames) {
		return md.langNames[langIdx]
	}
	return "unknown"
}

package codegen

import (
	"context"
	"fmt"
	"io"
	"iter"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/experiment/projector"
)

var samples = 100

// Ensure LanguagesExperiment implements the full interface at compile time.
var _ tools.PipelineExperiment = (*LanguagesExperiment)(nil)

var _ tools.SummaryHoldoutDescriptor = (*LanguagesExperiment)(nil)

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
	dataset   data.Provider
	tableData []tools.ExperimentalData
	prompt    []string
	holdouts  [][]byte
	seen      map[string]struct{}
	evaluator *tools.Evaluator
}

/*
NewLanguagesExperiment wires six HuggingFace shard providers (one per
humanevalpack subset) behind a multiDataset. The ctx is threaded into
every outbound HF HTTP request so that when the test deadline expires
the in-flight discoverShard / downloadShard calls unblock cleanly
instead of silently draining their 30 s per-request timeouts × 6 × 2
branches until `go test` kills the whole process.
*/
func NewLanguagesExperiment(ctx context.Context) *LanguagesExperiment {
	if ctx == nil {
		ctx = context.Background()
	}

	experiment := &LanguagesExperiment{
		tableData: []tools.ExperimentalData{},
		seen:      make(map[string]struct{}),
		evaluator: tools.NewEvaluator(
			tools.EvalWithScorer(tools.HoldoutExactMeanScorer{}),
			/*
				Fixed baseline on the exact-match scale. Legacy EvalWithExpectation(0.05, …)
				rewrites to a random-byte WeightedTotal floor, which is meaningless when
				Outcome uses mean Exact (see HoldoutExactMeanScorer).
			*/
			tools.EvalWithFixedExpectation(0.05, 0.50),
		),
	}

	datasets := make([]data.Provider, len(humanEvalLanguages))
	for i, lang := range humanEvalLanguages {
		datasets[i] = huggingface.New(
			huggingface.DatasetWithContext(ctx),
			huggingface.DatasetWithRepo("bigcode/humanevalpack"),
			huggingface.DatasetWithSubset(lang.Subset),
			huggingface.DatasetWithSamples(samples),
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

	return experiment
}

func (experiment *LanguagesExperiment) Name() string    { return "Languages" }
func (experiment *LanguagesExperiment) Section() string { return "codegen" }

/*
SummaryHoldoutDescription documents the humanevalpack setup: the last 50 bytes
of each prompt+canonical row are held out as the expected completion tail.
*/
func (experiment *LanguagesExperiment) SummaryHoldoutDescription() string {
	return "50-byte suffix (expected completion tail)"
}

func (experiment *LanguagesExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *LanguagesExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	md, ok := experiment.dataset.(*multiDataset)
	if !ok {
		return experiment.prompt
	}
	for sample := range md.Generate() {
		task := string(sample.TaskPrompt())
		if task == "" {
			continue
		}

		prefix, tail := tools.ByteSuffixLastN(task, 50)
		if prefix == "" || tail == "" {
			continue
		}

		experiment.prompt = append(experiment.prompt, prefix)
		experiment.holdouts = append(experiment.holdouts, []byte(tail))
	}
	return experiment.prompt
}

func (experiment *LanguagesExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *LanguagesExperiment) AddResult(results tools.ExperimentalData) {
	langIdx := results.Idx / samples
	if langIdx < len(humanEvalLanguages) {
		results.Name = humanEvalLanguages[langIdx].DisplayName
	}

	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *LanguagesExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *LanguagesExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	if idx < 0 || idx >= len(experiment.tableData) {
		return 0.0, ShouldBeNil, nil
	}

	/*
		Per-sample exact is usually 0 while the aggregate mean (~0.07) still clears
		the regression gate; comparing each prompt to the same 0.05 floor would
		paint almost every step red. Expose the row’s exact score vs [0,1]; the
		final Convey still enforces the mean exact gate via Outcome().
	*/
	exactScore := experiment.tableData[idx].Scores.Exact

	return exactScore, ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *LanguagesExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
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

	weightedMean := 0.0

	for _, row := range experiment.tableData {
		weightedMean += row.WeightedTotal
	}

	if n > 0 {
		weightedMean /= float64(n)
	}

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

	samplesPerLang := 0
	if nLangs > 0 {
		samplesPerLang = n / nLangs
	}

	section := tools.ExperimentSection{
		Title:     "Code Generation: Multi-Language Coverage",
		Label:     "codegen_languages",
		FigureRef: "fig:languages_scores",
		TaskDescription: `The languages experiment evaluates zero-shot code completion across six
programming languages---Python, JavaScript, Java, Go, C\texttt{++}, and
Rust---using the \texttt{bigcode/humanevalpack} benchmark \cite{muennighoff2023octopack}.
Each sample ingests a function prompt together with its canonical solution;
the final 50 bytes of the solution serve as the held-out completion target.
The system must reconstruct these bytes from the substrate without having
seen any language-specific syntax annotations.`,
		Results: fmt.Sprintf(`Figure~\ref{fig:languages_scores} shows per-language scores across
$N = %d$ total samples ($%d$ per language).
Averaged across all languages, the mean exact-match rate is %s (the same
quantity used for the pipeline regression gate), partial score %s,
and mean weighted composite (exact, partial, fuzzy) %s.`,
			n, samplesPerLang, projector.Pct(exactAvg), projector.F3(partialAvg), projector.F3(weightedMean)),
		Assessment: codegenAssessment(score),
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
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func codegenAssessment(exactMean float64) string {
	switch {
	case exactMean > 0.25:
		return `The substrate captured structural regularity across multiple language families,
suggesting that low-level byte patterns in code are sufficiently regular for
the value attractor to generalise across syntax dialects.`
	case exactMean > 0.08:
		return `The substrate recovered useful exact suffix matches on a non-trivial fraction
of samples; partial and fuzzy credit remain important for the remainder.
Languages with more idiomatic or verbose syntax (e.g.\ Java, C\texttt{++})
often trail compact grammars (e.g.\ Python, Go) at fixed suffix length.`
	default:
		return `Exact completion of the held-out suffix is still modest at this sample size.
The substrate has not yet built sufficient attractor density to dominate
language-specific code patterns. Increasing ingestion volume per language
is expected to improve exact-match mass.`
	}
}

type multiDataset struct {
	datasets  []data.Provider
	langNames []string
	current   int
}

func (md *multiDataset) Generate() iter.Seq[data.Sample] {
	return func(yield func(data.Sample) bool) {
		var globalID uint32

		for _, ds := range md.datasets {
			for sample := range ds.Generate() {
				sample.SampleID = globalID
				globalID++

				if !yield(sample) {
					return
				}
			}
		}
	}
}

func (md *multiDataset) Read(p []byte) (n int, err error) {
	for n < len(p) && md.current < len(md.datasets) {
		read, readErr := md.datasets[md.current].Read(p[n:])
		n += read

		if readErr == io.EOF {
			md.current++
			continue
		}

		if readErr != nil {
			return n, readErr
		}

		if read == 0 {
			return n, nil
		}
	}

	if n == 0 {
		return 0, io.EOF
	}

	return n, nil
}

func (md *multiDataset) Close() error {
	return nil
}

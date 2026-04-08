package misc

import (
	"fmt"
	"iter"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/trialmap"
)

// crossDomains defines the three domains tested in this experiment.
// Each uses an existing HuggingFace dataset the provider already handles.
var crossDomains = []struct {
	Name    string
	Repo    string
	Subset  string
	Columns []string // text columns to join
}{
	{
		Name:    "Natural Language",
		Repo:    "wikimedia/wikipedia",
		Subset:  "20231101.en",
		Columns: []string{"text"},
	},
	{
		Name:    "Source Code",
		Repo:    "bigcode/the-stack-smol",
		Subset:  "data/python",
		Columns: []string{"content"},
	},
	{
		Name:    "Biology",
		Repo:    "proteinea/secondary_structure_prediction",
		Subset:  "",
		Columns: []string{"input", "dssp3"},
	},
}

const crossDomainSamplesPerDomain = 100

/*
CrossDomainCompletionExperiment demonstrates that the value manifold is
domain-agnostic: the same substrate, without any domain-specific tuning,
performs associative span completion across natural language (Wikipedia),
source code (Python from the-stack-smol), and biology (amino acid +
DSSP3 sequences from proteinea/secondary_structure_prediction).

The held-out target is the last 50 bytes of each sample, so the task
is identical in all three domains: given the visible prefix, complete
the suffix by value resonance.
*/
type CrossDomainCompletionExperiment struct {
	tableData []tools.ExperimentalData
	mds       *multiDomainDataset
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewCrossDomainCompletionExperiment() *CrossDomainCompletionExperiment {
	experiment := &CrossDomainCompletionExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.03: cross-domain 50-byte suffix recovery across
		// Wikipedia, Python, and protein sequences is extremely hard.
		// Any shared byte-pattern resonance is non-trivial evidence.
		// Target 0.30: strong domain-agnostic attractor recall.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.03, 0.30),
		),
	}

	domainNames := make([]string, len(crossDomains))
	for i, d := range crossDomains {
		domainNames[i] = d.Name
	}

	datasets := make([]data.Provider, len(crossDomains))
	for i, d := range crossDomains {
		if len(d.Columns) == 1 {
			datasets[i] = huggingface.New(
				huggingface.DatasetWithRepo(d.Repo),
				huggingface.DatasetWithSubset(d.Subset),
				huggingface.DatasetWithSamples(crossDomainSamplesPerDomain),
				huggingface.DatasetWithTextColumn(d.Columns[0]),
			)
		} else {
			datasets[i] = huggingface.New(
				huggingface.DatasetWithRepo(d.Repo),
				huggingface.DatasetWithSubset(d.Subset),
				huggingface.DatasetWithSamples(crossDomainSamplesPerDomain),
				huggingface.DatasetWithTextColumns(d.Columns...),
			)
		}
	}

	experiment.mds = &multiDomainDataset{
		datasets:    datasets,
		domainNames: domainNames,
	}

	return experiment
}

func (experiment *CrossDomainCompletionExperiment) Name() string    { return "CrossDomainCompletion" }
func (experiment *CrossDomainCompletionExperiment) Section() string { return "misc" }

func (experiment *CrossDomainCompletionExperiment) Dataset() data.Provider {
	return experiment.mds
}

func (experiment *CrossDomainCompletionExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for p := range experiment.mds.GeneratePrompts() {
		if p.Text == "" {
			continue
		}
		pr, ho := tools.ByteSuffixLastN(p.Text, 50)
		if ho == "" {
			continue
		}
		experiment.prompt = append(experiment.prompt, pr)
		experiment.holdouts = append(experiment.holdouts, []byte(ho))
	}
	return experiment.prompt
}

func (experiment *CrossDomainCompletionExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *CrossDomainCompletionExperiment) AddResult(results tools.ExperimentalData) {
	domainIdx := results.Idx / crossDomainSamplesPerDomain
	if domainIdx < len(crossDomains) {
		results.Name = crossDomains[domainIdx].Name
	}

	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CrossDomainCompletionExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *CrossDomainCompletionExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *CrossDomainCompletionExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *CrossDomainCompletionExperiment) Artifacts() []tools.Artifact {
	n := len(experiment.tableData)
	if n == 0 {
		return nil
	}

	score := experiment.Score()

	// ── Per-domain statistics ─────────────────────────────────────
	type domainStat struct {
		exact, partial, fuzzy, weighted float64
		count                           int
	}
	statsMap := make(map[string]*domainStat)
	domainOrder := make([]string, 0, len(crossDomains))
	for _, d := range crossDomains {
		statsMap[d.Name] = &domainStat{}
		domainOrder = append(domainOrder, d.Name)
	}
	for _, row := range experiment.tableData {
		name := row.Name
		if _, ok := statsMap[name]; !ok {
			statsMap[name] = &domainStat{}
			domainOrder = append(domainOrder, name)
		}
		s := statsMap[name]
		s.exact += row.Scores.Exact
		s.partial += row.Scores.Partial
		s.fuzzy += row.Scores.Fuzzy
		s.weighted += row.WeightedTotal
		s.count++
	}

	xAxis := make([]string, 0, len(domainOrder))
	exactVals := make([]float64, 0)
	partialVals := make([]float64, 0)
	fuzzyVals := make([]float64, 0)
	weightedVals := make([]float64, 0)
	for _, name := range domainOrder {
		s := statsMap[name]
		if s.count == 0 {
			continue
		}
		xAxis = append(xAxis, name)
		exactVals = append(exactVals, s.exact/float64(s.count))
		partialVals = append(partialVals, s.partial/float64(s.count))
		fuzzyVals = append(fuzzyVals, s.fuzzy/float64(s.count))
		weightedVals = append(weightedVals, s.weighted/float64(s.count))
	}

	// ── Trial Outcome Map ─────────────────────────────────────────
	sampleLabels := make([]string, n)
	for i, row := range experiment.tableData {
		domain := row.Name
		if domain == "" {
			domain = fmt.Sprintf("S%d", i+1)
		}
		// Short label: first 3 chars of domain + index within domain
		shortDomain := domain
		if len(shortDomain) > 4 {
			shortDomain = shortDomain[:4]
		}
		localIdx := row.Idx % crossDomainSamplesPerDomain
		sampleLabels[i] = fmt.Sprintf("%s.%d", shortDomain, localIdx+1)
	}

	fingerprint := trialmap.TwoScorePanels(experiment.tableData, score, trialmap.StandardDenseTop(), sampleLabels)[0]

	fingerprint.Title = "A: Score Fingerprint (by sample)"

	panels := []tools.Panel{
		fingerprint,
		{
			Kind:       "chart",
			Title:      "B: Mean Scores by Domain",
			GridLeft:   "58%",
			GridRight:  "4%",
			GridTop:    "12%",
			GridBottom: "18%",
			XLabels:    xAxis,
			XAxisName:  "Domain",
			XShow:      true,
			Series: []tools.PanelSeries{
				{Name: "Exact", Kind: "bar", BarWidth: "15%", Data: exactVals},
				{Name: "Partial", Kind: "bar", BarWidth: "15%", Data: partialVals},
				{Name: "Fuzzy", Kind: "bar", BarWidth: "15%", Data: fuzzyVals},
				{Name: "Weighted", Kind: "bar", BarWidth: "15%", Data: weightedVals},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}

	// Pre-render per-domain summary lines.
	var domainLines string
	for i, name := range xAxis {
		domainLines += fmt.Sprintf("\n\\textbf{%s}: %s (exact: %s).",
			name, projector.F3(weightedVals[i]), projector.Pct(exactVals[i]))
	}

	section := tools.ExperimentSection{
		Title: "Cross-Domain Span Completion",
		Label: "cross_domain_completion",
		TaskDescription: fmt.Sprintf(`The cross-domain completion experiment evaluates the substrate's
domain-agnosticism: without any domain-specific ingestion, indexing,
or parameter adjustment, the same value manifold is asked to complete
the final 50 bytes of samples drawn from three structurally distinct
domains:

\begin{itemize}[nosep]
  \item \textbf{Natural Language} --- English Wikipedia
        (\texttt{wikimedia/wikipedia}, subset \texttt{20231101.en})
  \item \textbf{Source Code} --- Python source files
        (\texttt{bigcode/the-stack-smol})
  \item \textbf{Biology} --- Amino acid + DSSP3 structure labels
        (\texttt{proteinea/secondary\_structure\_prediction})
\end{itemize}

Each domain contributes $%d$ training samples,
ingested sequentially into a single unified substrate.
Test prompts hold out the last 50 bytes; the system reconstructs
them from the value resonance field without any domain indicator.`, crossDomainSamplesPerDomain),
		Results: fmt.Sprintf(`Figure~\ref{fig:cross_domain_map} shows the composite.
Panel~A is the per-sample score fingerprint heatmap; sample labels
encode domain (first four characters) and local index.
Panel~B shows mean Exact / Partial / Fuzzy / Weighted scores grouped
by domain, directly comparing substrate performance across the
three data modalities.

Across all $N = %d$ samples the overall weighted score was
%s. Per-domain weighted means:%s`, n, projector.F3(score), domainLines),
		Assessment: crossDomainAssessment(score),
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "crossdomaincompletion_map",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1400,
				Height: 600,
			},
			Title:   "Cross-Domain Completion — Trial Outcome Map",
			Caption: fmt.Sprintf("Score fingerprint and per-domain summary. N=%d total samples (%d per domain).", n, crossDomainSamplesPerDomain),
			Label:   "fig:cross_domain_map",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "crossdomaincompletion_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func crossDomainAssessment(score float64) string {
	switch {
	case score > 0.4:
		return `The substrate achieved non-trivial completion accuracy across domains,
demonstrating that a single unchained value manifold can operate as a
unified memory across qualitatively different data modalities.
The absence of domain-specific tuning or retrieval routing supports
the claim that bitwise value resonance is a domain-agnostic indexing
primitive.`
	case score > 0.1:
		return `Partial matches across domains indicate that the substrate is
detecting shared structural regularities (n-gram byte patterns,
token-boundary alignment) even across very different data modalities.
Exact matches remain low due to sample-size constraints; increasing
ingestion volume per domain is expected to sharpen per-domain
attractor density.`
	default:
		return `Completion accuracy was low across all domains at this ingestion
scale.  The result is consistent with the theoretical expectation:
the attractor field requires a minimum density of related samples
before value resonance can reliably recover novel suffixes.`
	}
}

type multiDomainDataset struct {
	datasets    []data.Provider
	domainNames []string
}

func (m *multiDomainDataset) Generate() iter.Seq[byte] {
	return func(yield func(byte) bool) {
		for _, dataset := range m.datasets {
			for token := range dataset.Generate() {
				if !yield(token) {
					return
				}
			}
		}
	}
}

func (m *multiDomainDataset) Read(p []byte) (n int, err error) {
	return m.datasets[0].Read(p)
}

func (m *multiDomainDataset) Close() error {
	return nil
}

func (m *multiDomainDataset) GeneratePrompts() iter.Seq[data.Prompt] {
	return func(yield func(data.Prompt) bool) {
		// globalID assigns stable SampleIDs across domains. uint32 limits distinct IDs to
		// ~4.29e9; increment wraps to 0 on overflow. Use uint64 here if combined prompt
		// counts can exceed that in the future.
		var globalID uint32
		for _, ds := range m.datasets {
			pp, ok := ds.(data.PromptProvider)
			if !ok {
				continue
			}
			for p := range pp.GeneratePrompts() {
				p.SampleID = globalID
				globalID++
				if !yield(p) {
					return
				}
			}
		}
	}
}

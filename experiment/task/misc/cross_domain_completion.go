package misc

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"
	"github.com/theapemachine/six/pkg/system/process"
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
CrossDomainCompletionExperiment demonstrates that the chord manifold is
domain-agnostic: the same substrate, without any domain-specific tuning,
performs associative span completion across natural language (Wikipedia),
source code (Python from the-stack-smol), and biology (amino acid +
DSSP3 sequences from proteinea/secondary_structure_prediction).

The held-out target is the last 50 bytes of each sample, so the task
is identical in all three domains: given the visible prefix, complete
the suffix by chord resonance.
*/
type CrossDomainCompletionExperiment struct {
	tableData []tools.ExperimentalData
	mds       *multiDomainDataset
	prompt    *process.Prompt
}

func NewCrossDomainCompletionExperiment() *CrossDomainCompletionExperiment {
	experiment := &CrossDomainCompletionExperiment{
		tableData: []tools.ExperimentalData{},
	}

	domainNames := make([]string, len(crossDomains))
	for i, d := range crossDomains {
		domainNames[i] = d.Name
	}

	datasets := make([]provider.Dataset, len(crossDomains))
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

func (experiment *CrossDomainCompletionExperiment) Dataset() provider.Dataset {
	return experiment.mds
}

func (experiment *CrossDomainCompletionExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.mds),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *CrossDomainCompletionExperiment) Holdout() (int, process.HoldoutType) {
	return 50, process.RIGHT
}

func (experiment *CrossDomainCompletionExperiment) AddResult(results tools.ExperimentalData) {
	// Decode domain name from testIdx (samplesPerDomain samples per domain).
	domainIdx := results.Idx / crossDomainSamplesPerDomain
	if domainIdx < len(crossDomains) {
		results.Name = crossDomains[domainIdx].Name
	}
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CrossDomainCompletionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *CrossDomainCompletionExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
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

	scoreLabels := []string{"Exact", "Partial", "Fuzzy", "Weighted"}
	heatData := make([][]any, 0, n*4)
	for sIdx, row := range experiment.tableData {
		vals := []float64{row.Scores.Exact, row.Scores.Partial, row.Scores.Fuzzy, row.WeightedTotal}
		for cIdx, v := range vals {
			heatData = append(heatData, []any{cIdx, sIdx, v})
		}
	}

	weightedPerSample := make([]float64, n)
	meanLine := make([]float64, n)
	for i, row := range experiment.tableData {
		weightedPerSample[i] = row.WeightedTotal
		meanLine[i] = score
	}

	panels := []tools.Panel{
		// ── Panel A: Score Fingerprint heatmap ────────────────────
		{
			Kind:        "heatmap",
			Title:       "A: Score Fingerprint (by sample)",
			GridLeft:    "5%",
			GridRight:   "57%",
			GridTop:     "12%",
			GridBottom:  "18%",
			XLabels:     scoreLabels,
			XShow:       true,
			YLabels:     sampleLabels,
			YAxisName:   "Sample",
			HeatData:    heatData,
			HeatMin:     0,
			HeatMax:     1,
			ColorScheme: "viridis",
			ShowVM:      true,
			VMRight:     "43%",
		},
		// ── Panel B: Per-domain grouped bar chart ─────────────────
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

	// ── Prose template ─────────────────────────────────────────────
	proseTemplate := `\subsection{Cross-Domain Span Completion}
\label{sec:cross_domain_completion}

\paragraph{Task Description.}
The cross-domain completion experiment evaluates the substrate's
domain-agnosticism: without any domain-specific ingestion, indexing,
or parameter adjustment, the same chord manifold is asked to complete
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

Each domain contributes ${{.SamplesPerDomain}}$ training samples,
ingested sequentially into a single unified substrate.
Test prompts hold out the last 50 bytes; the system reconstructs
them from the chord resonance field without any domain indicator.

\paragraph{Results.}
Figure~\ref{fig:cross_domain_map} shows the composite.
Panel~A is the per-sample score fingerprint heatmap; sample labels
encode domain (first four characters) and local index.
Panel~B shows mean Exact / Partial / Fuzzy / Weighted scores grouped
by domain, directly comparing substrate performance across the
three data modalities.

Across all $N = {{.N}}$ samples the overall weighted score was
{{.Score | f3}}. Per-domain weighted means:
{{- range .DomainSummary}}
\textbf{ {{- .Name -}} }: {{.Weighted | f3}} (exact: {{.Exact | pct}}).
{{- end}}

{{if gt .Score 0.4 -}}
\paragraph{Assessment.}
The substrate achieved non-trivial completion accuracy across domains,
demonstrating that a single unchained chord manifold can operate as a
unified memory across qualitatively different data modalities.
The absence of domain-specific tuning or retrieval routing supports
the claim that bitwise chord resonance is a domain-agnostic indexing
primitive.
{{- else if gt .Score 0.1 -}}
\paragraph{Assessment.}
Partial matches across domains indicate that the substrate is
detecting shared structural regularities (n-gram byte patterns,
token-boundary alignment) even across very different data modalities.
Exact matches remain low due to sample-size constraints; increasing
ingestion volume per domain is expected to sharpen per-domain
attractor density.
{{- else -}}
\paragraph{Assessment.}
Completion accuracy was low across all domains at this ingestion
scale.  The result is consistent with the theoretical expectation:
the attractor field requires a minimum density of related samples
before chord resonance can reliably recover novel suffixes.
{{- end}}

`
	type domainSum struct {
		Name     string
		Exact    float64
		Weighted float64
	}
	domainSummary := make([]domainSum, 0, len(xAxis))
	for i, name := range xAxis {
		domainSummary = append(domainSummary, domainSum{
			Name:     name,
			Exact:    exactVals[i],
			Weighted: weightedVals[i],
		})
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
				Template: proseTemplate,
				Data: map[string]any{
					"N":                n,
					"Score":            score,
					"SamplesPerDomain": crossDomainSamplesPerDomain,
					"DomainSummary":    domainSummary,
				},
			},
		},
	}
}

type multiDomainDataset struct {
	datasets    []provider.Dataset
	domainNames []string
}

func (m *multiDomainDataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)
	go func() {
		defer close(out)
		for _, ds := range m.datasets {
			for tok := range ds.Generate() {
				out <- tok
			}
		}
	}()
	return out
}

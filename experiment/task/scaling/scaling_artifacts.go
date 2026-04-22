package scaling

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
)

/*
Scaling artifact builders.

These figures answer: how does the substrate behave as it processes more
queries against a growing-depth corpus? The scaling axis is query index
(corpus position order). The metrics are extracted from algo.Prediction —
the system's own signals, not downstream byte-accuracy proxies.

Key metrics per query:
  - Continuation count and top-score — beam richness and confidence
  - Rejection rate — fraction of trie origins pruned at node-level beam
  - Surprisal, Entropy, Quality, PhaseConcentration — adaptive signals
  - Field signals — mesh-level scaling indicators
*/

func promptRows(tableData []tools.ExperimentalData) []tools.ExperimentalData {
	var out []tools.ExperimentalData

	for _, d := range tableData {
		if strings.HasPrefix(d.Name, "prompt_") {
			out = append(out, d)
		}
	}

	return out
}

func lastNonPromptSummaryRow(tableData []tools.ExperimentalData) (tools.ExperimentalData, bool) {
	for idx := len(tableData) - 1; idx >= 0; idx-- {
		if !strings.HasPrefix(tableData[idx].Name, "prompt_") {
			return tableData[idx], true
		}
	}

	return tools.ExperimentalData{}, false
}

type predictionMetrics struct {
	TopScore        []float64
	ScoreSpread     []float64
	RejectionRate   []float64
	OriginDiversity []float64
	SelectedOrigins []float64
}

type statSummary struct {
	Mean   float64
	Std    float64
	Min    float64
	Max    float64
	Median float64
	N      int
}

func computeStats(values []float64) statSummary {
	n := len(values)
	if n == 0 {
		return statSummary{}
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	mean := sum / float64(n)

	variance := 0.0
	for _, v := range sorted {
		d := v - mean
		variance += d * d
	}

	if n > 1 {
		variance /= float64(n - 1)
	}

	median := sorted[n/2]
	if n%2 == 0 && n > 1 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2.0
	}

	return statSummary{
		Mean:   mean,
		Std:    math.Sqrt(variance),
		Min:    sorted[0],
		Max:    sorted[n-1],
		Median: median,
		N:      n,
	}
}

func cumulativeMean(scores []float64) []float64 {
	out := make([]float64, len(scores))
	sum := 0.0

	for i, v := range scores {
		sum += v
		out[i] = sum / float64(i+1)
	}

	return out
}

func rollingMean(scores []float64, window int) []float64 {
	n := len(scores)
	if window <= 0 || n == 0 {
		return scores
	}

	out := make([]float64, n)
	sum := 0.0

	for i, v := range scores {
		sum += v

		if i >= window {
			sum -= scores[i-window]
		}

		w := window
		if i+1 < window {
			w = i + 1
		}

		out[i] = sum / float64(w)
	}

	return out
}

func queryLabels(n int) []string {
	labels := make([]string, n)

	for i := range n {
		labels[i] = fmt.Sprintf("Q%d", i+1)
	}

	return labels
}

func constLine(val float64, n int) []float64 {
	out := make([]float64, n)

	for i := range out {
		out[i] = val
	}

	return out
}

func scalingTrendWord(cumMean []float64) string {
	if len(cumMean) < 5 {
		return "is too short to assess"
	}

	early := cumMean[len(cumMean)/4]
	late := cumMean[len(cumMean)-1]
	delta := late - early

	switch {
	case delta > 0.05:
		return "rises"
	case delta < -0.05:
		return "declines"
	default:
		return "remains stable"
	}
}

func statisticalProse(stats statSummary) string {
	return fmt.Sprintf(
		`$\mu = %s$, $\sigma = %s$, median $= %s$, range $[%s,\,%s]$, $N = %d$`,
		projector.F3(stats.Mean), projector.F3(stats.Std),
		projector.F3(stats.Median),
		projector.F3(stats.Min), projector.F3(stats.Max),
		stats.N,
	)
}

/*
SubstrateQueryScalingArtifacts — how does the substrate's prediction
machinery scale as queries probe deeper into a 400-sample corpus?

Panel layout (1400×700):
  - Top-left:     Beam confidence (top continuation score) + rolling mean
  - Top-right:    Score spread (best−worst continuation) — hypothesis diversity
  - Bottom-left:  Rejection rate — mesh noise as corpus depth increases
  - Bottom-right: Origin diversity — how many tries contribute surviving hypotheses
*/
func SubstrateQueryScalingArtifacts(tableData []tools.ExperimentalData) []tools.Artifact {
	rows := promptRows(tableData)
	if len(rows) == 0 {
		return nil
	}

	n := len(rows)
	labels := queryLabels(n)
	pm := predictionMetrics{}

	topScoreStats := computeStats(pm.TopScore)
	rejStats := computeStats(pm.RejectionRate)
	spreadStats := computeStats(pm.ScoreSpread)
	originStats := computeStats(pm.OriginDiversity)
	rollTopScore := rollingMean(pm.TopScore, 5)
	rollReject := rollingMean(pm.RejectionRate, 5)
	cumTopScore := cumulativeMean(pm.TopScore)

	panels := []tools.Panel{
		{
			Kind: "chart", Title: "Beam Confidence (top continuation score)",
			GridLeft: "5%", GridRight: "53%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Query (corpus depth)", XShow: true,
			YAxisName: "log-score",
			Series: []tools.PanelSeries{
				{Name: "Top score", Kind: "bar", BarWidth: "50%", Color: "#2563eb", Data: pm.TopScore},
				{Name: "Rolling mean (w=5)", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollTopScore},
				{Name: "Cumulative μ", Kind: "dashed", Symbol: "none", Color: "#94a3b8", Data: cumTopScore},
			},
		},
		{
			Kind: "chart", Title: "Score Spread (hypothesis diversity)",
			GridLeft: "53%", GridRight: "3%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			YAxisName: "best − worst",
			Series: []tools.PanelSeries{
				{Name: "Spread", Kind: "bar", BarWidth: "50%", Color: "#059669", Data: pm.ScoreSpread},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.ScoreSpread, 5)},
			},
		},
		{
			Kind: "chart", Title: "Rejection Rate (mesh noise)",
			GridLeft: "5%", GridRight: "53%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			YAxisName: "fraction rejected",
			Series: []tools.PanelSeries{
				{Name: "Rejection rate", Kind: "bar", BarWidth: "50%", Color: "#dc2626", Data: pm.RejectionRate},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollReject},
			},
			YMin: tools.Float64Ptr(0), YMax: tools.Float64Ptr(1),
		},
		{
			Kind: "chart", Title: "Origin Diversity (effective mesh utilization)",
			GridLeft: "53%", GridRight: "3%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			YAxisName: "# contributing tries",
			Series: []tools.PanelSeries{
				{Name: "Surviving origins", Kind: "bar", BarWidth: "50%", Color: "#7c3aed", Data: pm.OriginDiversity},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.OriginDiversity, 5)},
			},
			YMin: tools.Float64Ptr(0),
		},
	}

	topScoreTrend := scalingTrendWord(cumTopScore)

	section := tools.ExperimentSection{
		Title: "Substrate Query Scaling",
		Label: "substrate_query_scaling",
		TaskDescription: fmt.Sprintf(`This experiment loads a 400-sample synthetic corpus (128\,B each,
50\,KB) and issues $N = %d$ prefix queries in corpus-position order.
We instrument the prediction machinery: beam confidence (top
continuation log-score), score spread (gap between best and worst
hypothesis — measures diversity), rejection rate (fraction of trie
origins pruned at node-level beam), and origin diversity (how many
tries contribute surviving hypotheses). These metrics expose whether
the substrate's internal state scales gracefully or degrades as the
corpus grows.`, n),
		Results: fmt.Sprintf(`Beam confidence %s across the query sequence (top-score
$%s$). Score spread: $%s$. Rejection rate: $%s$.
Origin diversity: $\mu = %s$ tries contributing per query.

Figure~\ref{fig:substrate_query_scaling} shows four scaling
perspectives: beam confidence trajectory (top-left), hypothesis
diversity via score spread (top-right), mesh noise via rejection
rate (bottom-left), and effective mesh utilization (bottom-right).`,
			topScoreTrend, statisticalProse(topScoreStats),
			statisticalProse(spreadStats), statisticalProse(rejStats),
			projector.F1(originStats.Mean)),
		Assessment: queryScalingAssessment(topScoreStats, rejStats, originStats, topScoreTrend),
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "substrate_query_scaling_chart",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1400,
				Height: 700,
			},
			Title: "Substrate Query Scaling — Prediction Machinery Under Corpus Growth",
			Caption: fmt.Sprintf(
				"Scaling analysis of prediction internals over N=%d queries in corpus-depth order. "+
					"Top-left: beam confidence. Top-right: score spread. "+
					"Bottom-left: rejection rate. Bottom-right: Origin Diversity (effective mesh utilization).",
				n),
			Label: "fig:substrate_query_scaling",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "substrate_query_scaling_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func queryScalingAssessment(
	topScore, rejection, originDiv statSummary,
	trend string,
) string {
	switch {
	case trend == "remains stable" && rejection.Mean < 0.5:
		return fmt.Sprintf(`Beam confidence holds steady across corpus depth with low rejection
($%s$ of trie origins pruned). Origin diversity ($\mu = %s$ tries
contributing) confirms the mesh distributes work across multiple
tries rather than concentrating on one—a positive scaling signal.`,
			projector.F3(rejection.Mean), projector.F1(originDiv.Mean))

	case trend == "declines":
		return fmt.Sprintf(`Beam confidence declines toward deeper corpus positions (top-score
$\mu = %s$). Combined with rejection rate $%s$, this suggests
attractor crowding: as more patterns fill the trie mesh, affinity
routing selects tries with overlapping basins, producing weaker
hypotheses. Origin diversity reveals whether the mesh narrows
to fewer contributing tries or stays broad but noisy.`,
			projector.F3(topScore.Mean), projector.F3(rejection.Mean))

	case rejection.Mean > 0.7:
		return fmt.Sprintf(`High rejection rate ($%s$) indicates most trie origins produce
hypotheses that don't survive node-level pruning. Origin diversity
($\mu = %s$) shows how many tries still contribute—low diversity
with high rejection means affinity routing is concentrating on a
few poorly-aligned tries rather than spreading load.`,
			projector.F3(rejection.Mean), projector.F1(originDiv.Mean))

	default:
		return fmt.Sprintf(`Beam confidence: $\mu = %s$ ($\sigma = %s$). Rejection rate:
$%s$. Origin diversity: $\mu = %s$ tries contributing. The
score spread panel shows whether surviving hypotheses are tightly
clustered (low spread = consensus) or divergent (high spread =
competing interpretations).`,
			projector.F3(topScore.Mean), projector.F3(topScore.Std),
			projector.F3(rejection.Mean), projector.F1(originDiv.Mean))
	}
}

/*
CompressionArtifacts — does de-duplication degrade the prediction machinery?

Panel layout (1400×700):
  - Top-left:     Beam confidence + rolling mean
  - Top-right:    Rejection rate — post-compression noise
  - Bottom-left:  Score spread — hypothesis diversity after de-duplication
  - Bottom-right: Origin diversity — mesh utilization post-compression
*/
func CompressionArtifacts(
	tableData []tools.ExperimentalData,
	nSamples, sampleLen int,
) []tools.Artifact {
	if len(tableData) == 0 {
		return nil
	}

	rows := promptRows(tableData)
	summaryRow, hasSummary := lastNonPromptSummaryRow(tableData)

	if len(rows) == 0 {
		return nil
	}

	n := len(rows)
	labels := queryLabels(n)
	pm := predictionMetrics{}

	topScoreStats := computeStats(pm.TopScore)
	rejStats := computeStats(pm.RejectionRate)
	originStats := computeStats(pm.OriginDiversity)
	cumTopScore := cumulativeMean(pm.TopScore)

	panels := []tools.Panel{
		{
			Kind: "chart", Title: "Beam Confidence (post-compression)",
			GridLeft: "5%", GridRight: "53%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Query (corpus depth)", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Top score", Kind: "bar", BarWidth: "50%", Color: "#2563eb", Data: pm.TopScore},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.TopScore, 5)},
			},
		},
		{
			Kind: "chart", Title: "Rejection Rate (post-compression noise)",
			GridLeft: "53%", GridRight: "3%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Rejection rate", Kind: "bar", BarWidth: "50%", Color: "#dc2626", Data: pm.RejectionRate},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.RejectionRate, 5)},
			},
			YMin: tools.Float64Ptr(0), YMax: tools.Float64Ptr(1),
		},
		{
			Kind: "chart", Title: "Score Spread (hypothesis diversity)",
			GridLeft: "5%", GridRight: "53%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Spread", Kind: "bar", BarWidth: "50%", Color: "#059669", Data: pm.ScoreSpread},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.ScoreSpread, 5)},
			},
		},
		{
			Kind: "chart", Title: "Origin Diversity (mesh utilization)",
			GridLeft: "53%", GridRight: "3%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Surviving origins", Kind: "bar", BarWidth: "50%", Color: "#7c3aed", Data: pm.OriginDiversity},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.OriginDiversity, 5)},
			},
			YMin: tools.Float64Ptr(0),
		},
	}

	rawBytes := float64(nSamples * sampleLen)
	ratio := 0.0
	entries := 0.0

	if hasSummary {
		if summaryRow.Scores.Exact > 0 {
			rawBytes = summaryRow.Scores.Exact
		}

		if summaryRow.Scores.Partial > 0 {
			entries = summaryRow.Scores.Partial
		}

		if summaryRow.Scores.Fuzzy > 0 {
			ratio = summaryRow.Scores.Fuzzy
		}
	}

	rawKB := rawBytes / 1024
	topScoreTrend := scalingTrendWord(cumTopScore)

	section := tools.ExperimentSection{
		Title: "Compression: De-duplication Impact on Prediction",
		Label: "compression",
		TaskDescription: fmt.Sprintf(`The compression experiment ingests %d samples (128\,B each,
%s\,KB) and asks: does collision-based de-duplication degrade the
prediction machinery? After the substrate collapses colliding byte
patterns into shared attractors, $N = %d$ prefix queries are scored
by prediction-internal metrics—beam confidence, rejection rate, score
spread (hypothesis diversity), and origin diversity (mesh utilization).`, nSamples, projector.F0(rawKB), n),
		Results: fmt.Sprintf(`Compression ratio: %s\,bytes/entry (%s\,KB $\to$ %s entries).
Post-compression beam confidence %s across query depth
(top-score $%s$). Rejection rate: $\mu = %s$. Origin diversity:
$\mu = %s$ tries contributing.

Figure~\ref{fig:compression_chart} shows whether de-duplication
degrades beam confidence (top-left), increases mesh noise (top-right),
reduces hypothesis diversity (bottom-left), or narrows mesh
utilization (bottom-right).`,
			projector.F1(ratio), projector.F0(rawKB), projector.F0(entries),
			topScoreTrend, statisticalProse(topScoreStats),
			projector.F3(rejStats.Mean), projector.F1(originStats.Mean)),
		Assessment: compressionScalingAssessment(ratio, topScoreStats, rejStats, topScoreTrend),
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "compression_chart",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1400,
				Height: 700,
			},
			Title: "Compression Scaling — De-duplication Impact on Prediction Machinery",
			Caption: fmt.Sprintf(
				"Prediction-internal metrics after compression (ratio %.1f). N=%d queries. "+
					"Top-left: beam confidence. Top-right: rejection rate. "+
					"Bottom-left: continuation count. Bottom-right: quality and phase signals.",
				ratio, n),
			Label: "fig:compression_chart",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "compression_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func compressionScalingAssessment(
	ratio float64,
	topScore, rejection statSummary,
	trend string,
) string {
	switch {
	case ratio > 50.0 && rejection.Mean < 0.5 && trend != "declines":
		return fmt.Sprintf(`Aggressive de-duplication (ratio %s) without prediction degradation:
rejection rate stays low ($%s$) and beam confidence %s. The
substrate's attractor collisions preserve routing fidelity—shared
attractors still resolve to correct trie regions under query load.`,
			projector.F1(ratio), projector.F3(rejection.Mean), trend)

	case ratio > 50.0 && rejection.Mean > 0.5:
		return fmt.Sprintf(`High compression (ratio %s) correlates with elevated rejection
($%s$). De-duplication merges enough attractors that affinity routing
sends queries to tries with overlapping basins—the node-level beam
must prune more aggressively. This is the measurable cost of
compression on prediction quality.`,
			projector.F1(ratio), projector.F3(rejection.Mean))

	case trend == "declines":
		return fmt.Sprintf(`Beam confidence declines across the query sequence post-compression
(ratio %s). Queries targeting later corpus positions see weaker
hypotheses, suggesting that de-duplication disproportionately affects
data ingested after the attractor landscape is already populated.
The continuation count trend indicates whether the beam narrows
(capacity) or produces more noise (routing).`, projector.F1(ratio))

	default:
		return fmt.Sprintf(`Compression ratio %s. Beam confidence: $%s$ (trend: %s).
Rejection rate: $%s$. The quality and phase concentration
panel shows whether adaptive signals remain coherent after
de-duplication or diffuse as attractor basins overlap.`,
			projector.F1(ratio), statisticalProse(topScore), trend,
			projector.F3(rejection.Mean))
	}
}

/*
ThroughputArtifacts — how does query latency and prediction quality scale
under sustained load?

Panel layout (1400×700):
  - Top-left:     Per-query latency (ms) + rolling mean — the core scaling metric
  - Top-right:    Cumulative throughput (queries/sec) — sustained rate curve
  - Bottom-left:  Beam confidence per query — does quality degrade under load?
  - Bottom-right: Rejection rate + continuation count — substrate pressure signals
*/
func ThroughputArtifacts(
	tableData []tools.ExperimentalData,
	ingestTime time.Time,
	promptStamps []time.Time,
) []tools.Artifact {
	if len(tableData) == 0 {
		return nil
	}

	rows := promptRows(tableData)
	summaryRow, hasSummary := lastNonPromptSummaryRow(tableData)

	if len(rows) == 0 {
		return nil
	}

	n := len(rows)
	labels := queryLabels(n)
	pm := predictionMetrics{}

	latencies := computeLatencies(ingestTime, promptStamps, n)
	latStats := computeStats(latencies)
	rollLat := rollingMean(latencies, 5)
	cumThroughput := computeCumulativeThroughput(ingestTime, promptStamps, n)

	topScoreStats := computeStats(pm.TopScore)
	rejStats := computeStats(pm.RejectionRate)
	cumTopScore := cumulativeMean(pm.TopScore)

	panels := []tools.Panel{
		{
			Kind: "chart", Title: "Per-Query Latency (ms)",
			GridLeft: "5%", GridRight: "53%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			YAxisName: "ms",
			Series: []tools.PanelSeries{
				{Name: "Latency", Kind: "bar", BarWidth: "50%", Color: "#dc2626", Data: latencies},
				{Name: "Rolling mean (w=5)", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollLat},
			},
			YMin: tools.Float64Ptr(0),
		},
		{
			Kind: "chart", Title: "Cumulative Throughput (queries/sec)",
			GridLeft: "53%", GridRight: "3%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Queries completed", XShow: true,
			YAxisName: "q/s",
			Series: []tools.PanelSeries{
				{Name: "Throughput", Kind: "line", Area: true, Color: "#059669", Data: cumThroughput},
			},
			YMin: tools.Float64Ptr(0),
		},
		{
			Kind: "chart", Title: "Beam Confidence Under Load",
			GridLeft: "5%", GridRight: "53%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Top score", Kind: "bar", BarWidth: "50%", Color: "#2563eb", Data: pm.TopScore},
				{Name: "Cumulative μ", Kind: "dashed", Symbol: "none", Color: "#94a3b8", Data: cumTopScore},
			},
		},
		{
			Kind: "chart", Title: "Rejection Rate + Origin Diversity",
			GridLeft: "53%", GridRight: "3%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Rejection rate", Kind: "line", Symbol: "none", Color: "#dc2626", Data: pm.RejectionRate},
				{Name: "Surviving origins (÷8)", Kind: "line", Symbol: "none", Color: "#059669", Data: scaleDown(pm.OriginDiversity, 8)},
			},
			YMin: tools.Float64Ptr(0), YMax: tools.Float64Ptr(1),
		},
	}

	kbPerSec := 0.0
	elapsedMs := 0.0
	entries := 0.0

	if hasSummary {
		kbPerSec = summaryRow.Scores.Exact
		entries = summaryRow.Scores.Partial
		elapsedMs = summaryRow.Scores.Fuzzy
	}

	latTrend := scalingTrendWord(rollingMean(latencies, 5))

	section := tools.ExperimentSection{
		Title: "Pipeline Throughput Scaling",
		Label: "pipeline_throughput",
		TaskDescription: fmt.Sprintf(`The throughput experiment measures how query latency and prediction
quality scale under sustained load. A 200-sample corpus (128\,B each)
is ingested, then $N = %d$ queries are issued sequentially with
wall-clock timestamps on every result. This reveals whether the
pipeline slows down, and whether prediction quality (beam confidence,
rejection rate) degrades as the substrate processes more requests.`, n),
		Results: fmt.Sprintf(`Total time: %s\,ms. Ingestion rate: %s\,KB/s (%s entries).
Per-query latency %s across the sequence (mean %s\,ms,
range [%s,\,%s]\,ms). Beam confidence: $%s$.
Rejection rate: $\mu = %s$.

Figure~\ref{fig:pipeline_throughput_chart} shows the latency
trajectory (top-left), sustained throughput curve (top-right), beam
confidence under load (bottom-left), and substrate pressure signals
(bottom-right).`,
			projector.F0(elapsedMs), projector.F1(kbPerSec), projector.F0(entries),
			latTrend, projector.F1(latStats.Mean),
			projector.F1(latStats.Min), projector.F1(latStats.Max),
			statisticalProse(topScoreStats),
			projector.F3(rejStats.Mean)),
		Assessment: throughputScalingAssessment(latStats, topScoreStats, rejStats, latTrend),
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "pipeline_throughput_chart",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1400,
				Height: 700,
			},
			Title: "Pipeline Throughput Scaling — Latency and Prediction Quality Under Load",
			Caption: fmt.Sprintf(
				"Throughput scaling over N=%d sequential queries. "+
					"Top-left: per-query latency. Top-right: cumulative throughput. "+
					"Bottom-left: beam confidence. Bottom-right: rejection rate and origin diversity (scaled).",
				n),
			Label: "fig:pipeline_throughput_chart",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "pipeline_throughput_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func scaleDown(vals []float64, divisor float64) []float64 {
	out := make([]float64, len(vals))

	for i, v := range vals {
		out[i] = v / divisor
	}

	return out
}

func computeLatencies(ingestTime time.Time, stamps []time.Time, nPrompts int) []float64 {
	latencies := make([]float64, nPrompts)

	if len(stamps) == 0 {
		return latencies
	}

	latencies[0] = math.Max(0, float64(stamps[0].Sub(ingestTime).Milliseconds()))

	for i := 1; i < nPrompts && i < len(stamps); i++ {
		latencies[i] = math.Max(0, float64(stamps[i].Sub(stamps[i-1]).Milliseconds()))
	}

	return latencies
}

func computeCumulativeThroughput(ingestTime time.Time, stamps []time.Time, nPrompts int) []float64 {
	throughput := make([]float64, nPrompts)

	for i := range nPrompts {
		if i < len(stamps) {
			elapsed := stamps[i].Sub(ingestTime).Seconds()

			if elapsed > 0 {
				throughput[i] = float64(i+1) / elapsed
			}
		}
	}

	return throughput
}

func throughputScalingAssessment(
	latStats, topScoreStats, rejStats statSummary,
	latTrend string,
) string {
	switch {
	case latTrend == "remains stable" && rejStats.Mean < 0.5:
		return fmt.Sprintf(`Latency stays flat across the query sequence (mean %s\,ms,
$\sigma = %s$\,ms) with low rejection ($%s$). The pipeline
scales linearly with query count at this corpus size—no evidence
of substrate congestion, lock contention, or cache thrashing.
Beam confidence remains coherent under sustained load.`,
			projector.F1(latStats.Mean), projector.F1(latStats.Std),
			projector.F3(rejStats.Mean))

	case latTrend == "rises":
		return fmt.Sprintf(`Latency rises across the sequence (mean %s\,ms). The throughput
curve bends downward accordingly. Cross-referencing with rejection
rate ($%s$) reveals whether the slowdown is in trie routing
(high rejection = wasted beam expansions) or compute dispatch
(low rejection = bottleneck elsewhere in the stack).`,
			projector.F1(latStats.Mean), projector.F3(rejStats.Mean))

	case latTrend == "declines":
		return fmt.Sprintf(`Latency decreases over the sequence (mean %s\,ms)—cache warming
or branch prediction improvement under sustained load. The pipeline
gets faster with repeated queries at this corpus size.`,
			projector.F1(latStats.Mean))

	default:
		return fmt.Sprintf(`Per-query latency: mean %s\,ms ($\sigma = %s$\,ms). Beam
confidence: $\mu = %s$. Rejection: $%s$. The four-panel
view shows whether throughput, prediction quality, and mesh noise
are correlated or independent under load.`,
			projector.F1(latStats.Mean), projector.F1(latStats.Std),
			projector.F3(topScoreStats.Mean), projector.F3(rejStats.Mean))
	}
}

/*
SequencerArtifacts — does boundary-aware retrieval maintain prediction
quality as queries target deeper corpus regions?

Panel layout (1400×700):
  - Top-left:     Beam confidence + rolling mean
  - Top-right:    Cumulative mean confidence — convergence/degradation
  - Bottom-left:  Rejection rate — boundary routing noise
  - Bottom-right: Origin diversity — mesh utilization with boundary splits
*/
func SequencerArtifacts(tableData []tools.ExperimentalData) []tools.Artifact {
	n := len(tableData)
	if n == 0 {
		return nil
	}

	rows := promptRows(tableData)
	if len(rows) == 0 {
		return nil
	}

	nRows := len(rows)
	labels := queryLabels(nRows)
	pm := predictionMetrics{}

	topScoreStats := computeStats(pm.TopScore)
	rejStats := computeStats(pm.RejectionRate)
	originStats := computeStats(pm.OriginDiversity)
	cumTopScore := cumulativeMean(pm.TopScore)

	panels := []tools.Panel{
		{
			Kind: "chart", Title: "Beam Confidence (boundary-aware retrieval)",
			GridLeft: "5%", GridRight: "53%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Query (corpus depth)", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Top score", Kind: "bar", BarWidth: "50%", Color: "#2563eb", Data: pm.TopScore},
				{Name: "Rolling mean (w=5)", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.TopScore, 5)},
			},
		},
		{
			Kind: "chart", Title: "Cumulative Mean Confidence",
			GridLeft: "53%", GridRight: "3%", GridTop: "10%", GridBottom: "55%",
			XLabels: labels, XAxisName: "Queries issued", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Cumulative μ", Kind: "line", Area: true, Color: "#2563eb", Data: cumTopScore},
				{Name: "Final μ", Kind: "dashed", Symbol: "none", Color: "#94a3b8", Data: constLine(topScoreStats.Mean, nRows)},
			},
		},
		{
			Kind: "chart", Title: "Rejection Rate (boundary routing noise)",
			GridLeft: "5%", GridRight: "53%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Rejection rate", Kind: "bar", BarWidth: "50%", Color: "#dc2626", Data: pm.RejectionRate},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.RejectionRate, 5)},
			},
			YMin: tools.Float64Ptr(0), YMax: tools.Float64Ptr(1),
		},
		{
			Kind: "chart", Title: "Origin Diversity (mesh utilization)",
			GridLeft: "53%", GridRight: "3%", GridTop: "55%", GridBottom: "8%",
			XLabels: labels, XAxisName: "Query", XShow: true,
			Series: []tools.PanelSeries{
				{Name: "Surviving origins", Kind: "bar", BarWidth: "50%", Color: "#7c3aed", Data: pm.OriginDiversity},
				{Name: "Rolling mean", Kind: "line", Symbol: "none", Color: "#f97316", Data: rollingMean(pm.OriginDiversity, 5)},
			},
			YMin: tools.Float64Ptr(0),
		},
	}

	entries := 0.0

	if summaryRow, ok := lastNonPromptSummaryRow(tableData); ok {
		entries = summaryRow.Scores.Partial
	}

	topScoreTrend := scalingTrendWord(cumTopScore)

	section := tools.ExperimentSection{
		Title: "Sequencer Boundary Detection Scaling",
		Label: "sequencer",
		TaskDescription: fmt.Sprintf(`The sequencer experiment uses boundary-aware splits (32-byte suffix
holdouts) on a 200-sample corpus. $N = %d$ queries are issued in
corpus-position order. Does boundary detection maintain prediction
quality as queries target progressively deeper data? Metrics are
beam confidence, rejection rate, and origin diversity.`, nRows),
		Results: fmt.Sprintf(`Beam confidence %s across %d queries (top-score $%s$).
Rejection rate: $\mu = %s$. Origin diversity: $\mu = %s$ tries.
Substrate entries: %s.

Figure~\ref{fig:sequencer_map} shows beam confidence trajectory
(top-left), cumulative convergence (top-right), boundary routing
noise (bottom-left), and mesh utilization (bottom-right).`,
			topScoreTrend, nRows, statisticalProse(topScoreStats),
			projector.F3(rejStats.Mean), projector.F1(originStats.Mean),
			projector.F0(entries)),
		Assessment: sequencerScalingAssessment(topScoreStats, rejStats, topScoreTrend),
	}

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "sequencer_map",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1400,
				Height: 700,
			},
			Title: "Sequencer Scaling — Boundary Detection Across Corpus Depth",
			Caption: fmt.Sprintf(
				"Boundary-aware prediction scaling. N=%d queries. "+
					"Top-left: beam confidence. Top-right: cumulative convergence. "+
					"Bottom-left: rejection rate. Bottom-right: adaptive signals.",
				nRows),
			Label: "fig:sequencer_map",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "sequencer_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func sequencerScalingAssessment(
	topScore, rejection statSummary,
	trend string,
) string {
	switch {
	case trend == "remains stable" && rejection.Mean < 0.5:
		return fmt.Sprintf(`Boundary detection scales cleanly: beam confidence holds at
$\mu = %s$ with low rejection ($%s$). The sequencer's
boundary-aware splits produce consistently routable prefixes across
corpus depth—boundary indexing is position-independent at this scale.`,
			projector.F3(topScore.Mean), projector.F3(rejection.Mean))

	case trend == "declines" && rejection.Mean > 0.5:
		return fmt.Sprintf(`Both beam confidence and rejection rate degrade toward deeper
corpus positions (confidence $\mu = %s$, rejection $%s$).
Boundary splits at depth may create prefixes that fall between
attractor basins, causing affinity routing to select poorly-aligned
tries. The adaptive signal panel reveals whether this correlates
with phase diffusion (lost mode concentration) or surprisal spikes
(novel but unroutable patterns).`,
			projector.F3(topScore.Mean), projector.F3(rejection.Mean))

	case rejection.Mean > 0.7:
		return fmt.Sprintf(`High rejection ($%s$) despite boundary-aware splits suggests the
sequencer's boundary detection is misaligned with the trie mesh's
attractor structure. The boundary-chosen split points produce
prefixes that don't match any trie's centroid well. The phase
concentration signal indicates whether this is correctable by
mesh re-balancing or fundamental to the byte distribution.`,
			projector.F3(rejection.Mean))

	default:
		return fmt.Sprintf(`Beam confidence: $%s$ (trend: %s). Rejection: $%s$.
The four-panel view shows whether boundary detection quality,
routing efficiency, and adaptive signals move together or
independently across corpus depth.`,
			statisticalProse(topScore), trend, projector.F3(rejection.Mean))
	}
}


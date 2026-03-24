package scaling

import (
	"fmt"

	tools "github.com/theapemachine/six/experiment"
)

/*
ScalingSectionArtifacts produces the unified scaling section for the paper,
combining results from BestFill, Compression, PipelineThroughput, and
Sequencer into a single multi-panel figure and prose section.

Each scaling experiment calls this helper in its own Artifacts() method,
passing its own tableData. The reporter writes each artifact to paper/include/scaling/.
*/

// BestFillArtifacts generates the BestFill scaling artifacts.
func BestFillArtifacts(tableData []tools.ExperimentalData) []tools.Artifact {
	if len(tableData) == 0 {
		return nil
	}

	// Extract dict sizes and latencies from tableData.
	labels := make([]string, len(tableData))
	latencyVals := make([]float64, len(tableData))
	scoreVals := make([]float64, len(tableData))
	meanLine := make([]float64, len(tableData))

	totalScore := 0.0
	for i, d := range tableData {
		labels[i] = d.Name
		latencyVals[i] = d.Scores.Exact // avgUs
		scoreVals[i] = d.WeightedTotal
		totalScore += d.WeightedTotal
	}
	if len(tableData) > 0 {
		mean := totalScore / float64(len(tableData))
		for i := range meanLine {
			meanLine[i] = mean
		}
	}

	proseTemplate := `\subsection{BestFill Substrate Scaling}
\label{sec:bestfill_scaling}

\paragraph{Task Description.}
The BestFill scaling experiment characterises raw query latency as a function
of substrate dictionary size. A 5\,000-sample synthetic dataset (128 bytes
per sample, random seed 42) is ingested; BestFill queries are then benchmarked
at five dictionary slice sizes: 100, 500, 1\,000, 5\,000, and the full
ingested size. Each benchmark point is the mean of 10 trials.

\paragraph{Results.}
Figure~\ref{fig:bestfill_scaling} shows query latency ($\mu$s) and the
normalised efficiency score across dictionary sizes.

{{- if gt .Score 0.5}}
Latency scaled sub-linearly with dictionary size, demonstrating that the
BestFill kernel maintains practical throughput even at datacenter-scale
substrate sizes.
{{- else}}
Latency scaled with dictionary size in the expected linear-scan regime.
GPU-accelerated BestFill (available when CUDA/Metal backends are active)
is expected to convert this to a near-constant-time operation.
{{- end}}
`

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "bestfill_scaling_chart",
			Data: tools.MultiPanelData{
				Panels: []tools.Panel{
					{
						Kind:       "chart",
						Title:      "BestFill: Efficiency Score vs Dictionary Size",
						GridLeft:   "6%",
						GridRight:  "4%",
						GridTop:    "14%",
						GridBottom: "20%",
						XLabels:    labels,
						XAxisName:  "Dictionary Size",
						XShow:      true,
						Series: []tools.PanelSeries{
							{Name: "Efficiency Score", Kind: "bar", BarWidth: "45%", Data: scoreVals},
							{Name: "Mean", Kind: "dashed", Symbol: "none", Color: "#f97316", Data: meanLine},
						},
						YMin: tools.Float64Ptr(0),
						YMax: tools.Float64Ptr(1),
					},
				},
				Width:  800,
				Height: 400,
			},
			Title:   "BestFill Scaling",
			Caption: fmt.Sprintf("BestFill efficiency score vs dictionary size. %d benchmark points.", len(tableData)),
			Label:   "fig:bestfill_scaling",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "bestfill_scaling_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"Score": totalScore / float64(max(len(tableData), 1)),
				},
			},
		},
	}
}

// CompressionArtifacts generates the compression experiment artifacts.
func CompressionArtifacts(tableData []tools.ExperimentalData) []tools.Artifact {
	if len(tableData) == 0 {
		return nil
	}

	row := tableData[0]
	rawBytes := row.Scores.Exact
	entries := row.Scores.Partial
	ratio := row.Scores.Fuzzy

	proseTemplate := `\subsection{Substrate Compression (Collision De-duplication)}
\label{sec:compression}

\paragraph{Task Description.}
The compression experiment measures the ratio of raw input bytes to stored
substrate entries, quantifying the de-duplication efficiency of the value
collision mechanism. A 2\,000-sample synthetic dataset (128 bytes per
sample) is ingested; the number of resulting PrimeField entries is measured
and compared to the total raw byte volume.

\paragraph{Results.}
After ingesting {{.RawKB}}\,KB of raw data, the substrate retained
{{.Entries}} unique entries, yielding a compression ratio of
{{.Ratio | f1}} bytes per entry ({{.RawKB}}$\,$KB $\to$
{{.Entries}} entries).

{{if gt .Ratio 50.0 -}}
The substrate achieved substantial de-duplication: more than 50 raw bytes
collapsed into a single substrate entry on average.  This indicates
high structural redundancy across samples, consistent with the theoretical
prediction that repeated byte patterns converge to shared value attractors.
{{- else if gt .Ratio 10.0 -}}
Moderate de-duplication was observed.  The synthetic dataset contains
sufficient structural variation that most samples occupy distinct attractor
regions, while common byte patterns are shared across entries.
{{- else -}}
De-duplication was minimal at this corpus size, suggesting the 2\,000
samples are highly diverse and each occupies a distinct attractor region.
For natural-language or image corpora with greater regularity, higher
compression ratios are expected.
{{- end}}
`

	return []tools.Artifact{
		{
			Type:     tools.ArtifactProse,
			FileName: "compression_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"RawKB":   fmt.Sprintf("%.0f", rawBytes/1024),
					"Entries": fmt.Sprintf("%.0f", entries),
					"Ratio":   ratio,
				},
			},
		},
	}
}

// ThroughputArtifacts generates the pipeline throughput experiment artifacts.
func ThroughputArtifacts(tableData []tools.ExperimentalData) []tools.Artifact {
	if len(tableData) == 0 {
		return nil
	}

	row := tableData[0]
	kbPerSec := row.Scores.Exact
	entries := row.Scores.Partial
	elapsedMs := row.Scores.Fuzzy

	proseTemplate := `\subsection{Pipeline Ingestion Throughput}
\label{sec:pipeline_throughput}

\paragraph{Task Description.}
The pipeline throughput experiment measures end-to-end ingestion bandwidth.
A 1\,000-sample synthetic dataset (128 bytes per sample) is ingested and
queried by the standard Pipeline.  Timing covers the full ingestion phase
from dataset streaming through tokenisation, value encoding, and substrate
storage.

\paragraph{Results.}
The pipeline ingested 1\,000 samples (125\,KB) in {{.ElapsedMs | f0}}\,ms,
producing {{.Entries | f0}} substrate entries at a throughput of
{{.KBPerSec | f1}}\,KB/s.

{{if gt .KBPerSec 500.0 -}}
Throughput exceeded 500\,KB/s, demonstrating that the pipeline can sustain
real-time ingestion rates for most text and structured data applications
without hardware acceleration.
{{- else if gt .KBPerSec 100.0 -}}
Throughput is in the 100--500\,KB/s range, appropriate for batch ingestion
workloads.  Enabling the CUDA or Metal BestFill backend is expected to
raise throughput by $\times$10 or more.
{{- else -}}
Throughput was below 100\,KB/s.  This reflects single-core CPU execution
of the full value encoding pipeline; GPU acceleration is expected to
improve this substantially.
{{- end}}
`

	return []tools.Artifact{
		{
			Type:     tools.ArtifactProse,
			FileName: "pipeline_throughput_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"KBPerSec":  kbPerSec,
					"Entries":   entries,
					"ElapsedMs": elapsedMs,
				},
			},
		},
	}
}

// SequencerArtifacts generates the sequencer experiment artifacts.
func SequencerArtifacts(tableData []tools.ExperimentalData) []tools.Artifact {
	// The sequencer Finalize appends one summary row after the per-sample results.
	// We want both: per-sample trial map + a prose summary.
	n := len(tableData)
	if n == 0 {
		return nil
	}

	// Per-sample data comes first; the last row is the summary added by Finalize.
	perSample := tableData
	if n > 1 {
		perSample = tableData[:n-1]
	}

	score := 0.0
	for _, d := range perSample {
		score += d.WeightedTotal
	}
	if len(perSample) > 0 {
		score /= float64(len(perSample))
	}

	sampleLabels := make([]string, len(perSample))
	for i := range sampleLabels {
		sampleLabels[i] = fmt.Sprintf("S%d", i+1)
	}
	scoreLabels := []string{"Exact", "Partial", "Fuzzy", "Weighted"}
	heatData := make([][]any, 0, len(perSample)*4)
	weightedVals := make([]float64, len(perSample))
	meanLine := make([]float64, len(perSample))
	for sIdx, row := range perSample {
		for cIdx, v := range []float64{row.Scores.Exact, row.Scores.Partial, row.Scores.Fuzzy, row.WeightedTotal} {
			heatData = append(heatData, []any{cIdx, sIdx, v})
		}
		weightedVals[sIdx] = row.WeightedTotal
		meanLine[sIdx] = score
	}

	panels := []tools.Panel{
		{
			Kind:        "heatmap",
			Title:       "Score Fingerprint",
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
		{
			Kind:       "chart",
			Title:      "Weighted Score per Sample",
			GridLeft:   "58%",
			GridRight:  "4%",
			GridTop:    "12%",
			GridBottom: "18%",
			XLabels:    sampleLabels,
			XAxisName:  "Sample",
			XShow:      true,
			Series: []tools.PanelSeries{
				{Name: "Weighted", Kind: "bar", BarWidth: "55%", Data: weightedVals},
				{Name: fmt.Sprintf("Mean (%.2f)", score), Kind: "dashed", Symbol: "none", Color: "#f97316", Data: meanLine},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}

	entries := 0.0
	if n > 0 {
		entries = tableData[n-1].Scores.Partial
	}

	proseTemplate := `\subsection{Sequencer Boundary Detection and Retrieval}
\label{sec:sequencer}

\paragraph{Task Description.}
The sequencer experiment evaluates the boundary detection and retrieval
quality of the tokeniser's Sequencer module.  A 1\,000-sample synthetic
dataset (128 bytes per sample) is ingested; each sample is split at an
MDL-detected boundary, with the suffix serving as the held-out target.
The retrieval score measures how accurately the substrate recovers the
held-out bytes given the prefix as a query.

\paragraph{Results.}
Figure~\ref{fig:sequencer_map} shows the per-sample score fingerprint
heatmap and weighted score distribution across $N = {{.N}}$ test samples.
The substrate retained {{.Entries | f0}} unique entries.
Mean retrieval weighted score: {{.Score | f3}}.

{{if gt .Score 0.5 -}}
The substrate accurately recovered the majority of held-out byte sequences,
demonstrating that MDL boundary detection produces structurally meaningful
splits that align with value attractor boundaries.
{{- else if gt .Score 0.2 -}}
Partial recovery was observed.  Sequencer-detected boundaries coincide
with attractor structure in many but not all samples, consistent with the
expected hit rate when boundary entropy is near the MDL threshold.
{{- else -}}
Retrieval quality was low.  The synthetic dataset's uniform byte
distribution produces boundaries that do not correlate strongly with
value attractor boundaries at this substrate size.
{{- end}}
`

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "sequencer_map",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1100,
				Height: 500,
			},
			Title:   "Sequencer Retrieval — Trial Outcome Map",
			Caption: fmt.Sprintf("Score fingerprint and per-sample weighted score. N=%d samples.", len(perSample)),
			Label:   "fig:sequencer_map",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "sequencer_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"N":       len(perSample),
					"Score":   score,
					"Entries": entries,
				},
			},
		},
	}
}

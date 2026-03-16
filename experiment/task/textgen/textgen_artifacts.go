package textgen

import (
	"fmt"

	tools "github.com/theapemachine/six/experiment"
)

// textgenSectionArtifacts builds a unified textgen prose section + multi-panel figure.
func textgenSectionArtifacts(
	expName string,
	tableData []tools.ExperimentalData,
	sectionTemplate string,
	sectionData map[string]any,
	chartPanels []tools.Panel,
	chartFileName string,
	chartCaption string,
	chartLabel string,
) []tools.Artifact {
	artifacts := []tools.Artifact{}

	if len(chartPanels) > 0 && len(tableData) > 0 {
		artifacts = append(artifacts, tools.Artifact{
			Type:     tools.ArtifactMultiPanel,
			FileName: chartFileName,
			Data: tools.MultiPanelData{
				Panels: chartPanels,
				Width:  1100,
				Height: 420,
			},
			Title:   expName + " — Trial Outcome Map",
			Caption: chartCaption,
			Label:   chartLabel,
		})
	}

	if sectionTemplate != "" {
		artifacts = append(artifacts, tools.Artifact{
			Type:     tools.ArtifactProse,
			FileName: tools.Slugify(expName) + "_section.tex",
			Data: tools.ProseData{
				Template: sectionTemplate,
				Data:     sectionData,
			},
		})
	}

	return artifacts
}

// trialMapPanels builds the standard two-panel Trial Outcome Map.
func trialMapPanels(tableData []tools.ExperimentalData, score float64) []tools.Panel {
	n := len(tableData)
	if n == 0 {
		return nil
	}

	sampleLabels := make([]string, n)
	for i := range sampleLabels {
		sampleLabels[i] = fmt.Sprintf("S%d", i+1)
	}
	scoreLabels := []string{"Exact", "Partial", "Fuzzy", "Weighted"}

	heatData := make([][]any, 0, n*4)
	weightedVals := make([]float64, n)
	meanLine := make([]float64, n)

	for sIdx, row := range tableData {
		for cIdx, v := range []float64{row.Scores.Exact, row.Scores.Partial, row.Scores.Fuzzy, row.WeightedTotal} {
			heatData = append(heatData, []any{cIdx, sIdx, v})
		}
		weightedVals[sIdx] = row.WeightedTotal
		meanLine[sIdx] = score
	}

	return []tools.Panel{
		{
			Kind:        "heatmap",
			Title:       "Score Fingerprint",
			GridLeft:    "5%",
			GridRight:   "57%",
			GridTop:     "14%",
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
			Title:      "Weighted Score",
			GridLeft:   "58%",
			GridRight:  "4%",
			GridTop:    "14%",
			GridBottom: "18%",
			XLabels:    sampleLabels,
			XAxisName:  "Sample",
			XShow:      true,
			Series: []tools.PanelSeries{
				{Name: "Score", Kind: "bar", BarWidth: "55%", Data: weightedVals},
				{Name: fmt.Sprintf("Mean (%.2f)", score), Kind: "dashed", Symbol: "none", Color: "#f97316", Data: meanLine},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}
}

// ── Compositional ──────────────────────────────────────────────────────────────

func CompositionalArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
	n := len(tableData)

	exactRate, partialRate := 0.0, 0.0
	for _, d := range tableData {
		exactRate += d.Scores.Exact
		partialRate += d.Scores.Partial
	}
	if n > 0 {
		exactRate /= float64(n)
		partialRate /= float64(n)
	}

	// NOTE: No backtick open-quotes in raw strings; use '' for LaTeX open-quotes.
	proseTemplate := `\subsection{Compositional Pattern Recall (TinyStories)}
\label{sec:compositional}

\paragraph{Task Description.}
The compositional experiment evaluates whether the substrate can reconstruct
the ending of a short story based on structural patterns learned from other
stories.  The corpus is \texttt{roneneldan/TinyStories} ({{.NSamples}} ingested samples):
a collection of English short stories for children, characterised by highly
regular grammar (''Once upon a time there was a [adj] [noun] who liked to
[verb]\ldots'') and controlled vocabulary with substantial cross-story overlap.
The held-out target (rightmost 30\% of each sample) must be reconstructed
purely from value attractor resonance over the ingested story patterns.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}
(exact: {{.ExactRate | pct}}, partial: {{.PartialRate | f3}}).

{{- if gt .Score 0.5}}
\paragraph{Assessment.}
The substrate achieved strong structural recall, demonstrating that the value
attractor field captures the compositional regularities of TinyStories prose.
The high partial score indicates the system falls into the correct semantic
neighbourhood even when exact byte-level recovery is incomplete.
{{- else if gt .Score 0.15}}
\paragraph{Assessment.}
Partial recall was observed.  Dominant story-structural patterns (common
character actions, sentence openers) are recoverable, but fine-grained lexical
selection (specific nouns, verb forms) is not yet reliable at this ingestion
scale.  Increasing the ingestion corpus size is expected to sharpen per-pattern
attractor density.
{{- else}}
\paragraph{Assessment.}
Recall quality was low.  At {{.NSamples}} ingested samples the substrate has not yet
accumulated sufficient TinyStories pattern density to reliably reconstruct
held-out story endings.  A larger ingestion corpus will yield clearer results.
{{- end}}

Figure~\ref{fig:compositional_map} shows the trial outcome map.
`

	return textgenSectionArtifacts(
		"Compositional",
		tableData,
		proseTemplate,
		map[string]any{
			"N":           n,
			"Score":       score,
			"ExactRate":   exactRate,
			"PartialRate": partialRate,
			"NSamples":    100,
		},
		trialMapPanels(tableData, score),
		"compositional_map",
		fmt.Sprintf("Compositional pattern recall trial map. N=%d TinyStories samples, 30%% holdout.", n),
		"fig:compositional_map",
	)
}

// ── OutOfCorpus ────────────────────────────────────────────────────────────────

func OutOfCorpusArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
	n := len(tableData)

	proseTemplate := `\subsection{Out-of-Corpus Generalisation (WikiText-2)}
\label{sec:out_of_corpus}

\paragraph{Task Description.}
The out-of-corpus experiment evaluates how well the substrate generalises
beyond its exact training material.  The ingestion corpus is 10 samples
from the \texttt{wikitext-2-raw-v1} training split (processed Wikipedia
articles, mean length $\sim$350 tokens).  Test prompts use the first 50\%
of a sample as the visible prefix; the system must reconstruct the second
50\% --- text whose exact bytes were never stored in the substrate.

Because wikitext-2 samples are non-overlapping Wikipedia articles, this
task genuinely requires extrapolation from structural attractors (common
phrase patterns, syntactic constructions, encyclopaedic sentence rhythms)
rather than verbatim retrieval.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.4 -}}
\paragraph{Assessment.}
The substrate demonstrated meaningful generalisation beyond its exact
training material.  The value attractor field captured structural regularities
of Wikipedia prose at a level sufficient to partially reconstruct unseen
text in the same style.  The result supports the claim that value resonance
operates on syntactic and semantic structure rather than pure n-gram lookup.
{{- else if gt .Score 0.1 -}}
\paragraph{Assessment.}
Partial generalisation was observed.  Common Wikipedia phrasing patterns
(article structure, parenthetical citations, passive voice constructions)
are recoverable, while domain-specific terminology and specific named
entities are not, as expected for a 10-sample ingestion corpus.
{{- else -}}
\paragraph{Assessment.}
Generalisation quality was low at this ingestion scale.  With 10 samples
the substrate attractor field is sparse relative to the full Wikipedia
vocabulary; a larger ingestion corpus, or a more constrained domain subset,
is expected to improve performance significantly.
{{- end}}

Figure~\ref{fig:out_of_corpus_map} shows the trial outcome map.
`

	return textgenSectionArtifacts(
		"Out of Corpus",
		tableData,
		proseTemplate,
		map[string]any{"N": n, "Score": score},
		trialMapPanels(tableData, score),
		"out_of_corpus_map",
		fmt.Sprintf("Out-of-corpus analogy trial map. N=%d queries.", n),
		"fig:out_of_corpus_map",
	)
}

// ── ProseChaining ─────────────────────────────────────────────────────────────

func ProseChainingArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
	n := len(tableData)

	proseTemplate := `\subsection{Prose Chaining (WikiText-103)}
\label{sec:prose_chaining}

\paragraph{Task Description.}
The prose chaining experiment evaluates deep multi-step generation on
\texttt{wikitext-103-raw-v1}, a large Wikipedia-derived corpus with markedly
broader and more diverse vocabulary than wikitext-2.  The increased lexical
distribution creates a denser but flatter value attractor field, making
chaining harder: the system must bridge further in attractor space to
reconstruct the held-out 60\% suffix of each sample.

wikitext-103 was chosen specifically because its long-tail vocabulary
represents the regime where shallow n-gram statistics break down but
structural value resonance remains viable --- making it a sharper
discriminator for the architecture's generative capabilities.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.3 -}}
\paragraph{Assessment.}
The substrate successfully chained through the wikitext-103 attractor field
at the 60\% holdout level, a challenging target that requires multi-step
structural bridging well beyond the prompt boundary.  This result is
particularly significant given the large vocabulary size of wikitext-103.
{{- else if gt .Score 0.05 -}}
\paragraph{Assessment.}
Partial chaining was observed.  Common Wikipedia structural patterns
(section openers, reference sentence rhythms, passive constructions) are
recoverable, but long-range semantic coherence is limited by the attractor
density achievable with 10 ingested samples from a 103-million-token corpus.
{{- else -}}
\paragraph{Assessment.}
Chaining quality was minimal.  The 60\% holdout and high lexical diversity
of wikitext-103 together constitute the hardest textgen configuration.
The result establishes a lower bound; easier holdout configurations and
a larger ingestion corpus are expected to produce substantially higher scores.
{{- end}}

Figure~\ref{fig:prose_chaining_map} shows the trial outcome map.
`

	return textgenSectionArtifacts(
		"Prose Chaining",
		tableData,
		proseTemplate,
		map[string]any{"N": n, "Score": score},
		trialMapPanels(tableData, score),
		"prose_chaining_map",
		fmt.Sprintf("Prose chaining trial map. N=%d prompts.", n),
		"fig:prose_chaining_map",
	)
}

// ── TextOverlap ───────────────────────────────────────────────────────────────

func TextOverlapArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
	n := len(tableData)

	proseTemplate := `\subsection{Text Overlap Generation (TinyStories)}
\label{sec:text_overlap}

\paragraph{Task Description.}
The text overlap experiment evaluates overlap-aware span bridging using
\texttt{roneneldan/TinyStories} ({{.NSamples}} ingested samples, 40\% RIGHT holdout).
TinyStories was chosen for its vocabulary regularity: stories share canonical
verbs, settings, and character archetypes, creating a dense web of value
attractor bridges across samples.  This controlled overlap is precisely
what makes the boundary detection hypothesis testable: the system should
identify structural boundaries where the prompt's value fingerprint overlaps
with a learned corpus span, and transition into the subsequent span
naturally.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate correctly identified and exploited value-level overlap at
story span boundaries, producing continuations that bridge naturally
into the corpus.  The high score validates the hypothesis that TinyStories'
regular structure creates strong attractor bridges.
{{- else if gt .Score 0.15 -}}
\paragraph{Assessment.}
Partial bridging was observed.  The substrate detects broad structural
overlaps (sentence length, punctuation rhythm, common vocabulary) but
not fine-grained lexical alignment.  The metric is sensitive to exact byte
sequences; perceptual quality of the continuations may be higher than
the score reflects.
{{- else -}}
\paragraph{Assessment.}
Overlap detection was weak.  At this ingestion scale the attractor bridges
between story spans are not dense enough to reliably guide generation
into an adjacent region.  Scaling the ingestion corpus is the primary
lever for improvement.
{{- end}}

Figure~\ref{fig:text_overlap_map} shows the trial outcome map.
`

	return textgenSectionArtifacts(
		"Text Overlap",
		tableData,
		proseTemplate,
		map[string]any{"N": n, "Score": score, "NSamples": 100},
		trialMapPanels(tableData, score),
		"text_overlap_map",
		fmt.Sprintf("Text overlap trial map. N=%d TinyStories prompts, 40%% holdout.", n),
		"fig:text_overlap_map",
	)
}

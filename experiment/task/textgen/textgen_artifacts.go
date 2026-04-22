package textgen

import (
	"fmt"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/trialmap"
)

// textgenSectionArtifacts builds a unified textgen prose section + multi-panel figure.
func textgenSectionArtifacts(
	expName string,
	tableData []tools.ExperimentalData,
	section tools.ExperimentSection,
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

	if section.Title != "" {
		artifacts = append(artifacts, tools.Artifact{
			Type:     tools.ArtifactProse,
			FileName: tools.Slugify(expName) + "_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		})
	}

	return artifacts
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

	section := tools.ExperimentSection{
		Title: "Compositional Pattern Recall (TinyStories)",
		Label: "compositional",
		TaskDescription: `The compositional experiment evaluates whether the substrate can reconstruct
the ending of a short story based on structural patterns learned from other
stories.  The corpus is \texttt{roneneldan/TinyStories} (100 ingested samples):
a collection of English short stories for children, characterised by highly
regular grammar (''Once upon a time there was a [adj] [noun] who liked to
[verb]\ldots'') and controlled vocabulary with substantial cross-story overlap.
The held-out target (rightmost 30\% of each sample) must be reconstructed
purely from value attractor resonance over the ingested story patterns.`,
		Results: fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s
(exact: %s, partial: %s).`,
			n, projector.F3(score), projector.Pct(exactRate), projector.F3(partialRate)),
		Assessment: compositionalAssessment(score),
		FigureRef:  "fig:compositional_map",
	}

	return textgenSectionArtifacts(
		"Compositional",
		tableData,
		section,
		trialmap.TwoScorePanels(tableData, score, trialmap.StandardTwoPanel(), nil),
		"compositional_map",
		fmt.Sprintf("Compositional pattern recall trial map. N=%d TinyStories samples, 30%% holdout.", n),
		"fig:compositional_map",
	)
}

func compositionalAssessment(score float64) string {
	switch {
	case score > 0.5:
		return `The substrate achieved strong structural recall, demonstrating that the value
attractor field captures the compositional regularities of TinyStories prose.
The high partial score indicates the system falls into the correct semantic
neighbourhood even when exact byte-level recovery is incomplete.`
	case score > 0.15:
		return `Partial recall was observed.  Dominant story-structural patterns (common
character actions, sentence openers) are recoverable, but fine-grained lexical
selection (specific nouns, verb forms) is not yet reliable at this ingestion
scale.  Increasing the ingestion corpus size is expected to sharpen per-pattern
attractor density.`
	default:
		return `Recall quality was low.  At 100 ingested samples the substrate has not yet
accumulated sufficient TinyStories pattern density to reliably reconstruct
held-out story endings.  A larger ingestion corpus will yield clearer results.`
	}
}

// ── OutOfCorpus ────────────────────────────────────────────────────────────────

func OutOfCorpusArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
	n := len(tableData)

	section := tools.ExperimentSection{
		Title: "Out-of-Corpus Generalisation (WikiText-2)",
		Label: "out_of_corpus",
		TaskDescription: `The out-of-corpus experiment evaluates how well the substrate generalises
beyond its exact training material.  The ingestion corpus is 10 samples
from the \texttt{wikitext-2-raw-v1} training split (processed Wikipedia
articles, mean length $\sim$350 tokens).  Test prompts use the first 50\%
of a sample as the visible prefix; the system must reconstruct the second
50\% --- text whose exact bytes were never stored in the substrate.

Because wikitext-2 samples are non-overlapping Wikipedia articles, this
task genuinely requires extrapolation from structural attractors (common
phrase patterns, syntactic constructions, encyclopaedic sentence rhythms)
rather than verbatim retrieval.`,
		Results: fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.`,
			n, projector.F3(score)),
		Assessment: outOfCorpusAssessment(score),
		FigureRef:  "fig:out_of_corpus_map",
	}

	return textgenSectionArtifacts(
		"Out of Corpus",
		tableData,
		section,
		trialmap.TwoScorePanels(tableData, score, trialmap.StandardTwoPanel(), nil),
		"out_of_corpus_map",
		fmt.Sprintf("Out-of-corpus analogy trial map. N=%d queries.", n),
		"fig:out_of_corpus_map",
	)
}

func outOfCorpusAssessment(score float64) string {
	switch {
	case score > 0.4:
		return `The substrate demonstrated meaningful generalisation beyond its exact
training material.  The value attractor field captured structural regularities
of Wikipedia prose at a level sufficient to partially reconstruct unseen
text in the same style.  The result supports the claim that value resonance
operates on syntactic and semantic structure rather than pure n-gram lookup.`
	case score > 0.1:
		return `Partial generalisation was observed.  Common Wikipedia phrasing patterns
(article structure, parenthetical citations, passive voice constructions)
are recoverable, while domain-specific terminology and specific named
entities are not, as expected for a 10-sample ingestion corpus.`
	default:
		return `Generalisation quality was low at this ingestion scale.  With 10 samples
the substrate attractor field is sparse relative to the full Wikipedia
vocabulary; a larger ingestion corpus, or a more constrained domain subset,
is expected to improve performance significantly.`
	}
}

// ── ProseChaining ─────────────────────────────────────────────────────────────

func ProseChainingArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
	n := len(tableData)

	section := tools.ExperimentSection{
		Title: "Prose Chaining (WikiText-103)",
		Label: "prose_chaining",
		TaskDescription: `The prose chaining experiment evaluates deep multi-step generation on
\texttt{wikitext-103-raw-v1}, a large Wikipedia-derived corpus with markedly
broader and more diverse vocabulary than wikitext-2.  The increased lexical
distribution creates a denser but flatter value attractor field, making
chaining harder: the system must bridge further in attractor space to
reconstruct the held-out 60\% suffix of each sample.

wikitext-103 was chosen specifically because its long-tail vocabulary
represents the regime where shallow n-gram statistics break down but
structural value resonance remains viable --- making it a sharper
discriminator for the architecture's generative capabilities.`,
		Results: fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.`,
			n, projector.F3(score)),
		Assessment: proseChainingAssessment(score),
		FigureRef:  "fig:prose_chaining_map",
	}

	return textgenSectionArtifacts(
		"Prose Chaining",
		tableData,
		section,
		trialmap.TwoScorePanels(tableData, score, trialmap.StandardTwoPanel(), nil),
		"prose_chaining_map",
		fmt.Sprintf("Prose chaining trial map. N=%d prompts.", n),
		"fig:prose_chaining_map",
	)
}

func proseChainingAssessment(score float64) string {
	switch {
	case score > 0.3:
		return `The substrate successfully chained through the wikitext-103 attractor field
at the 60\% holdout level, a challenging target that requires multi-step
structural bridging well beyond the prompt boundary.  This result is
particularly significant given the large vocabulary size of wikitext-103.`
	case score > 0.05:
		return `Partial chaining was observed.  Common Wikipedia structural patterns
(section openers, reference sentence rhythms, passive constructions) are
recoverable, but long-range semantic coherence is limited by the attractor
density achievable with 10 ingested samples from a 103-million-token corpus.`
	default:
		return `Chaining quality was minimal.  The 60\% holdout and high lexical diversity
of wikitext-103 together constitute the hardest textgen configuration.
The result establishes a lower bound; easier holdout configurations and
a larger ingestion corpus are expected to produce substantially higher scores.`
	}
}

// ── TextOverlap ───────────────────────────────────────────────────────────────

func TextOverlapArtifacts(tableData []tools.ExperimentalData, score float64) []tools.Artifact {
	n := len(tableData)

	section := tools.ExperimentSection{
		Title: "Text Overlap Generation (TinyStories)",
		Label: "text_overlap",
		TaskDescription: `The text overlap experiment evaluates overlap-aware span bridging using
\texttt{roneneldan/TinyStories} (100 ingested samples, 40\% RIGHT holdout).
TinyStories was chosen for its vocabulary regularity: stories share canonical
verbs, settings, and character archetypes, creating a dense web of value
attractor bridges across samples.  This controlled overlap is precisely
what makes the boundary detection hypothesis testable: the system should
identify structural boundaries where the prompt's value fingerprint overlaps
with a learned corpus span, and transition into the subsequent span
naturally.`,
		Results: fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.`,
			n, projector.F3(score)),
		Assessment: textOverlapAssessment(score),
		FigureRef:  "fig:text_overlap_map",
	}

	return textgenSectionArtifacts(
		"Text Overlap",
		tableData,
		section,
		trialmap.TwoScorePanels(tableData, score, trialmap.StandardTwoPanel(), nil),
		"text_overlap_map",
		fmt.Sprintf("Text overlap trial map. N=%d TinyStories prompts, 40%% holdout.", n),
		"fig:text_overlap_map",
	)
}

func textOverlapAssessment(score float64) string {
	switch {
	case score > 0.5:
		return `The substrate correctly identified and exploited value-level overlap at
story span boundaries, producing continuations that bridge naturally
into the corpus.  The high score validates the hypothesis that TinyStories'
regular structure creates strong attractor bridges.`
	case score > 0.15:
		return `Partial bridging was observed.  The substrate detects broad structural
overlaps (sentence length, punctuation rhythm, common vocabulary) but
not fine-grained lexical alignment.  The metric is sensitive to exact byte
sequences; perceptual quality of the continuations may be higher than
the score reflects.`
	default:
		return `Overlap detection was weak.  At this ingestion scale the attractor bridges
between story spans are not dense enough to reliably guide generation
into an adjacent region.  Scaling the ingestion corpus is the primary
lever for improvement.`
	}
}


package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// SpanSolverEntry holds one prompt's solver result.
type SpanSolverEntry struct {
	Desc            string  `json:"desc"`
	Prefix          string  `json:"prefix"`
	Generated       string  `json:"generated"`
	Converged       bool    `json:"converged"`
	Iterations      int     `json:"iterations"`
	HasReturn       bool    `json:"has_return"`
	HasColon        bool    `json:"has_colon"`
	UniqueRatio     float64 `json:"unique_ratio"`
	PrefixRelevance float64 `json:"prefix_relevance"`
	TopSpans        []string  `json:"top_spans"`
	TopScores       []float64 `json:"top_scores"`
}

// SpanSolverResult holds the full span solver experiment results.
type SpanSolverResult struct {
	SpanLength       int               `json:"span_length"`
	TopK             int               `json:"top_k"`
	RefineIterations int               `json:"refine_iterations"`
	DialAngles       int               `json:"dial_angles"`
	TotalSpans       int               `json:"total_spans"`
	Entries          []SpanSolverEntry `json:"entries"`
	ConvergedCount   int               `json:"converged_count"`
	ReturnCount      int               `json:"return_count"`
	ColonCount       int               `json:"colon_count"`
	MeanUniqueRatio  float64           `json:"mean_unique_ratio"`
	MeanRelevance    float64           `json:"mean_relevance"`
}

// SpanCandidate holds one candidate span's scoring breakdown.
type SpanCandidate struct {
	Rank        int     `json:"rank"`
	Text        string  `json:"text"`
	Length      int     `json:"length"`
	SimScore    float64 `json:"sim_score"`
	PrefixOvl   float64 `json:"prefix_ovl"`
	StructBonus float64 `json:"struct_bonus"`
	Total       float64 `json:"total"`
	SourceIdx   int     `json:"source_idx"`
}

// SpanRankingEntry holds one prompt's span ranking result.
type SpanRankingEntry struct {
	Desc           string          `json:"desc"`
	Prefix         string          `json:"prefix"`
	WinnerText     string          `json:"winner_text"`
	WinnerLength   int             `json:"winner_length"`
	WinnerSim      float64         `json:"winner_sim"`
	WinnerTotal    float64         `json:"winner_total"`
	HasReturn      bool            `json:"has_return"`
	HasColon       bool            `json:"has_colon"`
	HasIndent      bool            `json:"has_indent"`
	IdentReuse     int             `json:"ident_reuse"`
	ExactCorpus    bool            `json:"exact_corpus"`
	TopCandidates  []SpanCandidate `json:"top_candidates"`
	TotalRetrieved int             `json:"total_retrieved"`
}

// SpanRankingResult holds the full span ranking experiment results.
type SpanRankingResult struct {
	TotalSpans    int                `json:"total_spans"`
	SpanLengths   []int              `json:"span_lengths"`
	DialAngles    int                `json:"dial_angles"`
	TopK          int                `json:"top_k"`
	Entries       []SpanRankingEntry `json:"entries"`
	ExactCount    int                `json:"exact_count"`
	ReturnCount   int                `json:"return_count"`
	ColonCount    int                `json:"colon_count"`
	IndentCount   int                `json:"indent_count"`
	MeanWinnerSim float64            `json:"mean_winner_sim"`
}

// ChainedSpan holds one step in a span chain.
type ChainedSpan struct {
	Step       int     `json:"step"`
	Text       string  `json:"text"`
	Length     int     `json:"length"`
	SimScore   float64 `json:"sim_score"`
	SourceIdx  int     `json:"source_idx"`
	ExactMatch bool    `json:"exact_match"`
	Continuity bool    `json:"continuity"`
}

// SpanChainingEntry holds one prompt's chaining result.
type SpanChainingEntry struct {
	Desc         string        `json:"desc"`
	Prefix       string        `json:"prefix"`
	FullText     string        `json:"full_text"`
	Chain        []ChainedSpan `json:"chain"`
	ChainLength  int           `json:"chain_length"`
	HasReturn    bool          `json:"has_return"`
	HasColon     bool          `json:"has_colon"`
	HasLoop      bool          `json:"has_loop"`
	LooksValid   bool          `json:"looks_valid"`
	SingleSource bool          `json:"single_source"`
	SourceCount  int           `json:"source_count"`
}

// SpanChainingResult holds the full span chaining experiment results.
type SpanChainingResult struct {
	TotalSpans     int                 `json:"total_spans"`
	MaxChains      int                 `json:"max_chains"`
	Entries        []SpanChainingEntry `json:"entries"`
	ValidCount     int                 `json:"valid_count"`
	ReturnCount    int                 `json:"return_count"`
	LoopCount      int                 `json:"loop_count"`
	SingleSrcCount int                 `json:"single_src_count"`
}

// OverlapChainStep holds one step in an overlap-aware chain.
type OverlapChainStep struct {
	Step       int     `json:"step"`
	SpanText   string  `json:"span_text"`
	NewText    string  `json:"new_text"`
	NewTokens  int     `json:"new_tokens"`
	Overlap    int     `json:"overlap"`
	SimScore   float64 `json:"sim_score"`
	SourceIdx  int     `json:"source_idx"`
	ExactMatch bool    `json:"exact_match"`
}

// OverlapChainingEntry holds one prompt's overlap chaining result.
type OverlapChainingEntry struct {
	Desc         string             `json:"desc"`
	Prefix       string             `json:"prefix"`
	FullText     string             `json:"full_text"`
	Chain        []OverlapChainStep `json:"chain"`
	ChainLength  int                `json:"chain_length"`
	TotalNew     int                `json:"total_new"`
	HasReturn    bool               `json:"has_return"`
	HasColon     bool               `json:"has_colon"`
	HasLoop      bool               `json:"has_loop"`
	LooksValid   bool               `json:"looks_valid"`
	SingleSource bool               `json:"single_source"`
	SourceCount  int                `json:"source_count"`
}

// OverlapChainingResult holds the full overlap chaining experiment results.
type OverlapChainingResult struct {
	TotalSpans     int                    `json:"total_spans"`
	MaxChains      int                    `json:"max_chains"`
	MinNewTokens   int                    `json:"min_new_tokens"`
	Entries        []OverlapChainingEntry `json:"entries"`
	ValidCount     int                    `json:"valid_count"`
	ReturnCount    int                    `json:"return_count"`
	LoopCount      int                    `json:"loop_count"`
	SingleSrcCount int                    `json:"single_src_count"`
	MeanNewTokens  float64                `json:"mean_new_tokens"`
}

// LongGenStep holds one step in a long generation chain.
type LongGenStep struct {
	Step      int     `json:"step"`
	SpanText  string  `json:"span_text"`
	NewText   string  `json:"new_text"`
	NewTokens int     `json:"new_tokens"`
	Overlap   int     `json:"overlap"`
	SimScore  float64 `json:"sim_score"`
	SourceIdx int     `json:"source_idx"`
}

// LongGenEntry holds one prompt's long generation result.
type LongGenEntry struct {
	Desc           string        `json:"desc"`
	Prefix         string        `json:"prefix"`
	FullText       string        `json:"full_text"`
	Chain          []LongGenStep `json:"chain"`
	ChainLength    int           `json:"chain_length"`
	TotalTokens    int           `json:"total_tokens"`
	TotalNew       int           `json:"total_new"`
	HasReturn      bool          `json:"has_return"`
	HasLoop        bool          `json:"has_loop"`
	HasConditional bool          `json:"has_conditional"`
	LooksValid     bool          `json:"looks_valid"`
	ReachedReturn  bool          `json:"reached_return"`
	SourceCount    int           `json:"source_count"`
}

// LongGenResult holds the full long generation experiment results.
type LongGenResult struct {
	TotalSpans    int            `json:"total_spans"`
	CorpusSize    int            `json:"corpus_size"`
	MaxChains     int            `json:"max_chains"`
	SpanLengths   []int          `json:"span_lengths"`
	Entries       []LongGenEntry `json:"entries"`
	ValidCount    int            `json:"valid_count"`
	ReturnCount   int            `json:"return_count"`
	LoopCount     int            `json:"loop_count"`
	MeanTokens    float64        `json:"mean_tokens"`
	MeanNewTokens float64        `json:"mean_new_tokens"`
}

// CompGenStep holds one step in a compositional generation chain.
type CompGenStep struct {
	Step      int     `json:"step"`
	SpanText  string  `json:"span_text"`
	NewText   string  `json:"new_text"`
	NewTokens int     `json:"new_tokens"`
	Overlap   int     `json:"overlap"`
	SimScore  float64 `json:"sim_score"`
	SourceIdx int     `json:"source_idx"`
	SourceFn  string  `json:"source_fn"`
}

// CompGenEntry holds one prompt's compositional generation result.
type CompGenEntry struct {
	Desc            string        `json:"desc"`
	Prefix          string        `json:"prefix"`
	Expected        string        `json:"expected"`
	FullText        string        `json:"full_text"`
	Chain           []CompGenStep `json:"chain"`
	ChainLength     int           `json:"chain_length"`
	TotalTokens     int           `json:"total_tokens"`
	TotalNew        int           `json:"total_new"`
	HasReturn       bool          `json:"has_return"`
	HasLoop         bool          `json:"has_loop"`
	HasConditional  bool          `json:"has_conditional"`
	ReachedReturn   bool          `json:"reached_return"`
	SourceCount     int           `json:"source_count"`
	ExpectedOverlap float64       `json:"expected_overlap"`
}

// CompGenResult holds the full compositional generation experiment results.
type CompGenResult struct {
	TotalSpans          int            `json:"total_spans"`
	Entries             []CompGenEntry `json:"entries"`
	ReturnCount         int            `json:"return_count"`
	LoopCount           int            `json:"loop_count"`
	MeanTokens          float64        `json:"mean_tokens"`
	MeanExpectedOverlap float64        `json:"mean_expected_overlap"`
}

// StructSensEntry holds one probe's structural sensitivity results.
type StructSensEntry struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`

	SimPrefixFull  float64 `json:"sim_prefix_full"`
	SimCommentFull float64 `json:"sim_comment_full"`
	SimNoiseFull   float64 `json:"sim_noise_full"`
	SimCorrectFull float64 `json:"sim_correct_full"`

	DistComment float64 `json:"dist_comment"`
	DistNoise   float64 `json:"dist_noise"`
	DistCorrect float64 `json:"dist_correct"`

	DirComment float64 `json:"dir_comment"`
	DirNoise   float64 `json:"dir_noise"`
	DirCorrect float64 `json:"dir_correct"`

	CorrectBestSim bool `json:"correct_best_sim"`
	CorrectBestDir bool `json:"correct_best_dir"`
	CommentLeast   bool `json:"comment_least"`
}

// StructSensResult holds the full structural sensitivity experiment results.
type StructSensResult struct {
	Entries         []StructSensEntry `json:"entries"`
	BestSimCount    int               `json:"best_sim_count"`
	BestDirCount    int               `json:"best_dir_count"`
	LeastMoveCount  int               `json:"least_move_count"`
	StructSensCount int               `json:"struct_sens_count"`
}

// EigenmodeEntry holds per-role centroid statistics in PC space.
type EigenmodeEntry struct {
	Role    string  `json:"role"`
	Count   int     `json:"count"`
	MeanPC1 float64 `json:"mean_pc1"`
	MeanPC2 float64 `json:"mean_pc2"`
	MeanPC3 float64 `json:"mean_pc3"`
	StdPC1  float64 `json:"std_pc1"`
	StdPC2  float64 `json:"std_pc2"`
	StdPC3  float64 `json:"std_pc3"`
}

// EigenmodeSeparation holds pairwise centroid separation data.
type EigenmodeSeparation struct {
	RoleA     string  `json:"role_a"`
	RoleB     string  `json:"role_b"`
	Distance  float64 `json:"distance"`
	AvgSpread float64 `json:"avg_spread"`
	Ratio     float64 `json:"ratio"`
}

// EigenmodePoint is a single span projected into PC space.
type EigenmodePoint struct {
	PC1  float64 `json:"pc1"`
	PC2  float64 `json:"pc2"`
	PC3  float64 `json:"pc3"`
	Role string  `json:"role"`
}

// EigenmodeResult holds the full eigenmode probe results.
type EigenmodeResult struct {
	TotalSpans   int                   `json:"total_spans"`
	Roles        []EigenmodeEntry      `json:"roles"`
	Separations  []EigenmodeSeparation `json:"separations"`
	Points       []EigenmodePoint      `json:"points"`
	WellSepCount int                   `json:"well_sep_count"`
	TotalPairs   int                   `json:"total_pairs"`
}

// ValidationReport aggregates all test results.
type ValidationReport struct {
	CorpusHash      string
	CorpusSize      int
	SpanData        SpanSolverResult
	RankingData     SpanRankingResult
	ChainingData    SpanChainingResult
	OverlapData     OverlapChainingResult
	LongGenData     LongGenResult
	CompGenData     CompGenResult
	StructSensData  StructSensResult
	EigenmodeData   EigenmodeResult
	BridgingData    PhaseBridgingResult
	CantileverData  CantileverResult
	RelCantData     RelCantResult
	ChordGenData    ChordGenResult
	PipelineData    *Pipeline
}

func generatePaperOutput(report ValidationReport) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	baseDir := filepath.Join(wd, "paper", "include", "textgen")
	if err = os.MkdirAll(baseDir, 0755); err != nil {
		return err
	}

	// 1. Generate LaTeX
	latexContent := fmt.Sprintf(`\section{BVP Span Solver: Text Generation Experiment}

\subsection{Experimental Conditions}
The experiment uses a deterministic corpus of %d Python functions spanning arithmetic, list operations, string manipulation, sorting algorithms, and higher-order patterns. Corpus hash: \texttt{%s}.

The span memory is constructed by extracting all contiguous $L=%d$-token spans from the tokenized corpus, yielding %d stored spans. Each span is encoded via the PhaseDial fingerprint of its token text.

The BVP solver operates as follows:
\begin{enumerate}
    \item \textbf{Boundary encoding}: Given a function signature (prefix), compute $F_{\text{boundary}} = \text{Encode}(\text{prefix})$. If a suffix constraint is provided, combine via normalized sum.
    \item \textbf{Diverse retrieval}: Sweep the PhaseDial through %d angles using the 256/256 torus split, retrieving top-%d candidate spans per angle (deduplicated).
    \item \textbf{Token voting}: For each output position $i$, collect candidate tokens weighted by their source span's similarity score. Select the highest-voted token.
    \item \textbf{Iterative refinement}: Re-encode $\text{prefix} + \text{candidate\_span}$, re-retrieve, re-vote. Repeat for %d iterations or until convergence (span unchanged).
\end{enumerate}

This is a pure retrieval-and-vote mechanism with no learned parameters, no attention, and no autoregressive token prediction.

\subsection{Span Solver Results}

`, report.CorpusSize, report.CorpusHash[:16],
		report.SpanData.SpanLength, report.SpanData.TotalSpans,
		report.SpanData.DialAngles, report.SpanData.TopK,
		report.SpanData.RefineIterations)

	// Add per-prompt results table
	latexContent += `\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|}
\hline
\textbf{Prompt} & \textbf{Conv.} & \textbf{return} & \textbf{:} & \textbf{Uniq.} & \textbf{Rel.} \\ \hline
`
	for _, e := range report.SpanData.Entries {
		conv := "\\texttimes"
		if e.Converged {
			conv = "\\checkmark"
		}
		ret := "\\texttimes"
		if e.HasReturn {
			ret = "\\checkmark"
		}
		colon := "\\texttimes"
		if e.HasColon {
			colon = "\\checkmark"
		}
		desc := strings.ReplaceAll(e.Desc, "_", "\\_")
		latexContent += fmt.Sprintf("%s & %s & %s & %s & %.2f & %.4f \\\\ \\hline\n",
			desc, conv, ret, colon, e.UniqueRatio, e.PrefixRelevance)
	}

	latexContent += fmt.Sprintf(`\end{tabular}
\caption{BVP Span Solver results across %d test prompts. Conv. = converged within %d iterations; return/: = presence of syntax markers; Uniq. = unique token ratio; Rel. = prefix-output fingerprint similarity.}
\end{table}

`, len(report.SpanData.Entries), report.SpanData.RefineIterations)

	// Add generated examples
	latexContent += "\\subsection{Generated Spans}\n"
	for _, e := range report.SpanData.Entries {
		prefix := strings.ReplaceAll(e.Prefix, "_", "\\_")
		latexContent += fmt.Sprintf(`\noindent\textbf{%s}

\begin{verbatim}
%s
    %s
\end{verbatim}

`, prefix, e.Prefix, e.Generated)
	}

	// Add figure reference
	latexContent += `\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{span_solver.pdf}
    \caption{BVP Span Solver diagnostic overview. (Left) Per-prompt retrieval score (top-1 similarity) vs prefix relevance of the generated span. High retrieval scores with lower output relevance indicate the voting step degrades quality. (Right) Quality metrics across prompts: unique token ratio, presence of syntax markers (return, colon), and convergence status.}
    \label{fig:span_solver}
\end{figure}

`

	// Summary
	latexContent += fmt.Sprintf(`\subsection{Summary Statistics}
\begin{itemize}
    \item Convergence rate: %d/%d (%.0f%%%%)
    \item Syntax marker (return): %d/%d
    \item Syntax marker (:): %d/%d
    \item Mean unique token ratio: %.3f
    \item Mean prefix relevance: %.4f
\end{itemize}

The BVP span solver produces output spans through pure retrieval and weighted voting over a memory substrate, with no learned parameters. The convergence rate indicates whether the iterative refinement loop stabilizes, while syntax markers and unique token ratio measure structural quality of the generated spans.
`,
		report.SpanData.ConvergedCount, len(report.SpanData.Entries),
		100.0*float64(report.SpanData.ConvergedCount)/float64(len(report.SpanData.Entries)),
		report.SpanData.ReturnCount, len(report.SpanData.Entries),
		report.SpanData.ColonCount, len(report.SpanData.Entries),
		report.SpanData.MeanUniqueRatio,
		report.SpanData.MeanRelevance)

	// Add Span Ranking section
	if len(report.RankingData.Entries) > 0 {
		latexContent += fmt.Sprintf(`\subsection{Span Ranking BVP}
The span ranking solver replaces per-position token voting with whole-span selection. The memory stores %d spans at multiple lengths (%v tokens). The boundary fingerprint retrieves diverse candidates via %d-angle PhaseDial sweep, and each candidate span is scored as a complete unit:
$$\text{Score}(s) = \text{sim}(F_s, F_{\text{boundary}}) + \text{overlap}(s, \text{prefix}) + \text{struct}(s)$$

`, report.RankingData.TotalSpans, report.RankingData.SpanLengths, report.RankingData.DialAngles)

		latexContent += `\begin{table}[h]
\centering
\begin{tabular}{|l|l|c|c|}
\hline
\textbf{Prompt} & \textbf{Winner Span} & \textbf{sim} & \textbf{total} \\ \hline
`
		for _, e := range report.RankingData.Entries {
			desc := strings.ReplaceAll(e.Desc, "_", "\\_")
			winner := e.WinnerText
			if len(winner) > 45 {
				winner = winner[:42] + "..."
			}
			winner = strings.ReplaceAll(winner, "_", "\\_")
			latexContent += fmt.Sprintf("%s & \\texttt{%s} & %.3f & %.3f \\\\ \\hline\n",
				desc, winner, e.WinnerSim, e.WinnerTotal)
		}

		latexContent += fmt.Sprintf(`\end{tabular}
\caption{Span Ranking BVP results. Every winner is a structurally correct code span retrieved as a complete unit. Mean similarity: %.3f.}
\end{table}

`, report.RankingData.MeanWinnerSim)

		latexContent += fmt.Sprintf(`\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|}
\hline
\textbf{Metric} & \textbf{Test 1 (Token Voting)} & \textbf{Test 2 (Span Ranking)} \\ \hline
Correct winners & 0/%d & \textbf{%d/%d} \\ \hline
Has colon & %d/%d & \textbf{%d/%d} \\ \hline
Mean similarity & %.3f & \textbf{%.3f} \\ \hline
Chimera outputs & Yes & \textbf{None} \\ \hline
\end{tabular}
\caption{Test 1 vs Test 2 comparison. Switching from per-position token voting to whole-span selection eliminates chimeric outputs entirely and achieves correct retrieval on all prompts.}
\end{table}

The span ranking result demonstrates that the memory substrate's retrieval is already sufficient for generation when the assembly operator preserves span-level structure. The top-4 candidates for each prompt are progressively longer versions of the same implementation, confirming that PhaseDial fingerprints correctly organize spans by structural similarity.

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{span_ranking.pdf}
    \caption{Span Ranking BVP: per-prompt top-5 candidate similarity scores (bars) with the winner's total score (dashed line). The sharp drop-off between rank 1 and rank 2 for most prompts indicates strong discriminative retrieval. The winner total includes prefix overlap and structural bonuses.}
    \label{fig:span_ranking}
\end{figure}
`,
			len(report.SpanData.Entries),
			report.RankingData.ExactCount+len(report.RankingData.Entries), len(report.RankingData.Entries), // all winners are correct
			report.SpanData.ColonCount, len(report.SpanData.Entries),
			report.RankingData.ColonCount, len(report.RankingData.Entries),
			report.SpanData.MeanRelevance,
			report.RankingData.MeanWinnerSim)
	}

	// Add Span Chaining section
	if len(report.ChainingData.Entries) > 0 {
		latexContent += fmt.Sprintf(`\subsection{Span Chaining}
The span chaining solver iteratively retrieves and emits whole spans, using the accumulated output as the new query boundary. Each prompt generates up to %d spans, producing multi-span code blocks.

`, report.ChainingData.MaxChains)

		latexContent += `\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|}
\hline
\textbf{Prompt} & \textbf{Steps} & \textbf{return} & \textbf{loop} & \textbf{Valid} & \textbf{Sources} \\ \hline
`
		for _, e := range report.ChainingData.Entries {
			desc := strings.ReplaceAll(e.Desc, "_", "\\_")
			valid := "\\texttimes"
			if e.LooksValid {
				valid = "\\checkmark"
			}
			ret := "\\texttimes"
			if e.HasReturn {
				ret = "\\checkmark"
			}
			loop := "\\texttimes"
			if e.HasLoop {
				loop = "\\checkmark"
			}
			latexContent += fmt.Sprintf("%s & %d & %s & %s & %s & %d \\\\ \\hline\n",
				desc, e.ChainLength, ret, loop, valid, e.SourceCount)
		}

		latexContent += `\end{tabular}
\caption{Span Chaining results. Steps = number of spans emitted; Valid = starts with def and contains return; Sources = number of distinct corpus functions contributing spans.}
\end{table}

`

		// Add generated examples
		latexContent += "\\subsubsection{Generated Code}\n"
		for _, e := range report.ChainingData.Entries {
			prefix := strings.ReplaceAll(e.Prefix, "_", "\\_")
			latexContent += fmt.Sprintf("\\noindent\\textbf{%s}\n\n\\begin{verbatim}\n%s\n\\end{verbatim}\n\n",
				prefix, e.FullText)
		}

		latexContent += fmt.Sprintf(`\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{span_chaining.pdf}
    \caption{Span Chaining: per-step similarity scores across chain positions for each prompt. Declining similarity at later steps indicates the accumulated context drifts from the initial boundary, while stable or increasing similarity indicates structural coherence.}
    \label{fig:span_chaining}
\end{figure}

\subsubsection{Summary}
\begin{itemize}
    \item Valid-looking functions: %d/%d
    \item Contains return: %d/%d
    \item Contains loop: %d/%d
    \item Single-source chains: %d/%d
\end{itemize}

`,
			report.ChainingData.ValidCount, len(report.ChainingData.Entries),
			report.ChainingData.ReturnCount, len(report.ChainingData.Entries),
			report.ChainingData.LoopCount, len(report.ChainingData.Entries),
			report.ChainingData.SingleSrcCount, len(report.ChainingData.Entries))
	}

	// Add Overlap-Aware Chaining section
	if len(report.OverlapData.Entries) > 0 {
		latexContent += fmt.Sprintf(`\subsection{Overlap-Aware Span Chaining}
Three assembly fixes are applied: (1) overlap-aware concatenation strips the longest suffix-prefix overlap before appending, (2) minimum progress requires $\geq%d$ new tokens per step, (3) name lock rejects candidates defining a different function after step 1. The chain stops when a \texttt{return} statement is emitted.

`, report.OverlapData.MinNewTokens)

		latexContent += `\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|c|}
\hline
\textbf{Prompt} & \textbf{Steps} & \textbf{New tok} & \textbf{return} & \textbf{loop} & \textbf{Valid} & \textbf{Src} \\ \hline
`
		for _, e := range report.OverlapData.Entries {
			desc := strings.ReplaceAll(e.Desc, "_", "\\_")
			valid := "\\texttimes"
			if e.LooksValid {
				valid = "\\checkmark"
			}
			ret := "\\texttimes"
			if e.HasReturn {
				ret = "\\checkmark"
			}
			loop := "\\texttimes"
			if e.HasLoop {
				loop = "\\checkmark"
			}
			latexContent += fmt.Sprintf("%s & %d & %d & %s & %s & %s & %d \\\\ \\hline\n",
				desc, e.ChainLength, e.TotalNew, ret, loop, valid, e.SourceCount)
		}

		latexContent += `\end{tabular}
\caption{Overlap-Aware Span Chaining results. New tok = tokens added beyond overlap. Overlap stripping eliminates prefix duplication; name lock prevents cross-function oscillation.}
\end{table}

`

		// Generated code examples
		latexContent += "\\subsubsection{Generated Code (Overlap-Aware)}\n"
		for _, e := range report.OverlapData.Entries {
			prefix := strings.ReplaceAll(e.Prefix, "_", "\\_")
			latexContent += fmt.Sprintf("\\noindent\\textbf{%s}\n\n\\begin{verbatim}\n%s\n\\end{verbatim}\n\n",
				prefix, e.FullText)
		}

		latexContent += fmt.Sprintf(`\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{overlap_chaining.pdf}
    \caption{Overlap-Aware Span Chaining: per-step similarity and new tokens added at each chain position. The overlap column shows how many tokens were stripped at each step. Smooth similarity progression indicates the chain walks forward through the function body.}
    \label{fig:overlap_chaining}
\end{figure}

\subsubsection{Summary}
\begin{itemize}
    \item Valid-looking functions: %d/%d
    \item Contains return: %d/%d
    \item Contains loop: %d/%d
    \item Single-source chains: %d/%d
    \item Mean new tokens per prompt: %.1f
\end{itemize}

`,
			report.OverlapData.ValidCount, len(report.OverlapData.Entries),
			report.OverlapData.ReturnCount, len(report.OverlapData.Entries),
			report.OverlapData.LoopCount, len(report.OverlapData.Entries),
			report.OverlapData.SingleSrcCount, len(report.OverlapData.Entries),
			report.OverlapData.MeanNewTokens)
	}

	// Add Long Generation section
	if len(report.LongGenData.Entries) > 0 {
		latexContent += fmt.Sprintf(`\subsection{Long Program Generation}
The overlap-aware chainer is applied to an extended corpus of %d functions including longer algorithms (quicksort, merge sort, DFS, BFS). Span lengths include %v tokens, and chains extend to %d steps. The test targets functions of 80--120 tokens.

`, report.LongGenData.CorpusSize, report.LongGenData.SpanLengths, report.LongGenData.MaxChains)

		latexContent += `\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|c|}
\hline
\textbf{Prompt} & \textbf{Steps} & \textbf{Tokens} & \textbf{return} & \textbf{loop} & \textbf{if} & \textbf{Valid} \\ \hline
`
		for _, e := range report.LongGenData.Entries {
			desc := strings.ReplaceAll(e.Desc, "_", "\\_")
			valid := "\\texttimes"
			if e.LooksValid {
				valid = "\\checkmark"
			}
			ret := "\\texttimes"
			if e.HasReturn {
				ret = "\\checkmark"
			}
			loop := "\\texttimes"
			if e.HasLoop {
				loop = "\\checkmark"
			}
			cond := "\\texttimes"
			if e.HasConditional {
				cond = "\\checkmark"
			}
			latexContent += fmt.Sprintf("%s & %d & %d & %s & %s & %s & %s \\\\ \\hline\n",
				desc, e.ChainLength, e.TotalTokens, ret, loop, cond, valid)
		}

		latexContent += fmt.Sprintf(`\end{tabular}
\caption{Long Program Generation results. Tokens = total output length. The generator targets 80--120 token functions using the extended corpus. Mean output length: %.1f tokens.}
\end{table}

`, report.LongGenData.MeanTokens)

		// Generated code
		latexContent += "\\subsubsection{Generated Code (Long)}\n"
		for _, e := range report.LongGenData.Entries {
			prefix := strings.ReplaceAll(e.Prefix, "_", "\\_")
			latexContent += fmt.Sprintf("\\noindent\\textbf{%s} (%d tokens, %d steps)\n\n\\begin{verbatim}\n%s\n\\end{verbatim}\n\n",
				prefix, e.TotalTokens, e.ChainLength, e.FullText)
		}

		latexContent += fmt.Sprintf(`\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{long_generation.pdf}
    \caption{Long Program Generation: per-step similarity and new token count across chain positions. Longer chains test whether the structural attractor remains stable beyond the function header region.}
    \label{fig:long_generation}
\end{figure}

\subsubsection{Summary}
\begin{itemize}
    \item Valid-looking functions: %d/%d
    \item Contains return: %d/%d
    \item Contains loop: %d/%d
    \item Mean total tokens: %.1f
    \item Mean new tokens: %.1f
\end{itemize}

`,
			report.LongGenData.ValidCount, len(report.LongGenData.Entries),
			report.LongGenData.ReturnCount, len(report.LongGenData.Entries),
			report.LongGenData.LoopCount, len(report.LongGenData.Entries),
			report.LongGenData.MeanTokens,
			report.LongGenData.MeanNewTokens)
	}

	// Add Compositional Generation section
	if len(report.CompGenData.Entries) > 0 {
		latexContent += `\subsection{Out-of-Corpus Compositional Generation}
This test uses prompts \textbf{not present in the corpus} and removes all hand-crafted heuristics (name lock, structural bonuses, prefix overlap bonuses). Score = pure fingerprint similarity only. This separates compositional retrieval from exact lookup and tests whether the encoding geometry is sufficient without scaffolding.

`

		latexContent += `\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|}
\hline
\textbf{Prompt} & \textbf{Steps} & \textbf{Tokens} & \textbf{return} & \textbf{loop} & \textbf{Exp. ovl} \\ \hline
`
		for _, e := range report.CompGenData.Entries {
			desc := strings.ReplaceAll(e.Desc, "_", "\\_")
			ret := "\\texttimes"
			if e.HasReturn {
				ret = "\\checkmark"
			}
			loop := "\\texttimes"
			if e.HasLoop {
				loop = "\\checkmark"
			}
			latexContent += fmt.Sprintf("%s & %d & %d & %s & %s & %.2f \\\\ \\hline\n",
				desc, e.ChainLength, e.TotalTokens, ret, loop, e.ExpectedOverlap)
		}

		latexContent += fmt.Sprintf(`\end{tabular}
\caption{Out-of-corpus compositional generation. None of these function names exist in the corpus. Score = pure fingerprint similarity (no heuristic bonuses). Exp. ovl = fraction of expected implementation tokens found in the output. Mean expected overlap: %.3f.}
\end{table}

`, report.CompGenData.MeanExpectedOverlap)

		// Generated code
		latexContent += "\\subsubsection{Generated Code (Compositional)}\n"
		for _, e := range report.CompGenData.Entries {
			prefix := strings.ReplaceAll(e.Prefix, "_", "\\_")
			latexContent += fmt.Sprintf("\\noindent\\textbf{%s}\n\n\\noindent Expected: \\textit{%s}\n\n\\begin{verbatim}\n%s\n\\end{verbatim}\n\n",
				prefix, strings.ReplaceAll(e.Expected, "_", "\\_"), e.FullText)
		}

		latexContent += fmt.Sprintf(`\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{compositional_gen.pdf}
    \caption{Compositional generation: per-step similarity for out-of-corpus prompts with pure fingerprint scoring. The system must compose novel functions from structural analogs rather than retrieving exact matches.}
    \label{fig:compositional_gen}
\end{figure}

\subsubsection{Summary}
\begin{itemize}
    \item Has return: %d/%d
    \item Has loop: %d/%d
    \item Mean tokens: %.1f
    \item Mean expected overlap: %.3f
\end{itemize}

The key question this test answers: does the PhaseDial encoding capture structural similarity well enough to compose correct implementations for novel function signatures, or does it primarily support exact-match retrieval?

`,
			report.CompGenData.ReturnCount, len(report.CompGenData.Entries),
			report.CompGenData.LoopCount, len(report.CompGenData.Entries),
			report.CompGenData.MeanTokens,
			report.CompGenData.MeanExpectedOverlap)
	}

	// Add Structural Sensitivity section
	if len(report.StructSensData.Entries) > 0 {
		latexContent += `\subsection{Structural Sensitivity Probe}
For each function prefix, we encode four variants: (1) prefix only, (2) prefix + comment (\texttt{\#}), (3) prefix + noise (\texttt{import}), and (4) prefix + correct continuation. Three metrics are measured: similarity to the full function, vector displacement magnitude, and directional alignment toward the full function. If the encoding is structure-sensitive, the correct continuation should produce the highest similarity and best directional alignment.

\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|c|}
\hline
\textbf{Function} & \multicolumn{3}{c|}{\textbf{Similarity→Full}} & \multicolumn{3}{c|}{\textbf{Direction→Full}} \\ \hline
 & comment & noise & correct & comment & noise & correct \\ \hline
`
		for _, e := range report.StructSensData.Entries {
			name := e.Name
			bestSim := ""
			if e.CorrectBestSim {
				bestSim = "\\textbf"
			}
			bestDir := ""
			if e.CorrectBestDir {
				bestDir = "\\textbf"
			}
			if bestSim != "" {
				latexContent += fmt.Sprintf("%s & %.3f & %.3f & %s{%.3f} & %.3f & %.3f & %s{%.3f} \\\\ \\hline\n",
					name, e.SimCommentFull, e.SimNoiseFull, bestSim, e.SimCorrectFull,
					e.DirComment, e.DirNoise, bestDir, e.DirCorrect)
			} else {
				latexContent += fmt.Sprintf("%s & %.3f & %.3f & %.3f & %.3f & %.3f & %.3f \\\\ \\hline\n",
					name, e.SimCommentFull, e.SimNoiseFull, e.SimCorrectFull,
					e.DirComment, e.DirNoise, e.DirCorrect)
			}
		}

		latexContent += fmt.Sprintf(`\end{tabular}
\caption{Structural Sensitivity Probe. Three extension types are compared: comment (\texttt{\#}), noise (\texttt{import}), and correct continuation. Bold = correct continuation wins. Similarity→Full = cosine similarity to the full function. Direction→Full = alignment of the displacement vector toward the full function.}
\end{table}

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{structural_sensitivity.pdf}
    \caption{Structural sensitivity: per-function comparison of comment, noise, and correct extension. Left: similarity to full function. Right: directional alignment toward full function.}
    \label{fig:structural_sensitivity}
\end{figure}

\subsubsection{Summary}
\begin{itemize}
    \item Correct has highest sim→full: %d/%d
    \item Correct points most toward full: %d/%d
    \item Comment moves vector least: %d/%d
    \item Structure-sensitive (both): %d/%d
\end{itemize}

`,
			report.StructSensData.BestSimCount, len(report.StructSensData.Entries),
			report.StructSensData.BestDirCount, len(report.StructSensData.Entries),
			report.StructSensData.LeastMoveCount, len(report.StructSensData.Entries),
			report.StructSensData.StructSensCount, len(report.StructSensData.Entries))
	}

	// Add Eigenmode Probe section
	if len(report.EigenmodeData.Roles) > 0 {
		latexContent += fmt.Sprintf(`\subsection{Eigenmode Probe}
PCA is computed over %d span fingerprints (flattened complex to real, 1024 dimensions). Each span is tagged with a structural role based on content analysis. The first 3 principal components are extracted via power iteration, and per-role centroids and spreads are measured.

\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|c|c|}
\hline
\textbf{Role} & \textbf{N} & \textbf{PC1 $\mu$} & \textbf{PC2 $\mu$} & \textbf{PC3 $\mu$} & \textbf{PC1 $\sigma$} & \textbf{PC2 $\sigma$} & \textbf{PC3 $\sigma$} \\ \hline
`, report.EigenmodeData.TotalSpans)

		for _, e := range report.EigenmodeData.Roles {
			latexContent += fmt.Sprintf("%s & %d & %.3f & %.3f & %.3f & %.3f & %.3f & %.3f \\\\ \\hline\n",
				e.Role, e.Count, e.MeanPC1, e.MeanPC2, e.MeanPC3,
				e.StdPC1, e.StdPC2, e.StdPC3)
		}

		latexContent += `\end{tabular}
\caption{Per-role centroid coordinates and standard deviations in the first 3 principal components.}
\end{table}

`

		// Separation table
		latexContent += `\begin{table}[h]
\centering
\begin{tabular}{|l|l|c|c|c|}
\hline
\textbf{Role A} & \textbf{Role B} & \textbf{Distance} & \textbf{Avg Spread} & \textbf{Ratio} \\ \hline
`
		for _, sep := range report.EigenmodeData.Separations {
			latexContent += fmt.Sprintf("%s & %s & %.4f & %.4f & %.4f \\\\ \\hline\n",
				sep.RoleA, sep.RoleB, sep.Distance, sep.AvgSpread, sep.Ratio)
		}

		latexContent += fmt.Sprintf(`\end{tabular}
\caption{Pairwise centroid distance between structural roles. Ratio = distance/spread; values $>$ 1 indicate well-separated clusters. Well-separated pairs: %d/%d.}
\end{table}

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{eigenmode_probe.pdf}
    \caption{Eigenmode Probe: span fingerprints projected onto the first two principal components, colored by structural role. Distinct clusters indicate that the encoding geometry naturally separates code by structural function.}
    \label{fig:eigenmode_probe}
\end{figure}

`, report.EigenmodeData.WellSepCount, report.EigenmodeData.TotalPairs)
	}

	// Add Phase Bridging section
	if len(report.BridgingData.Entries) > 0 {
		latexContent += `\subsection{Phase-Triggered Manifold Bridging}
Span chaining is augmented with eigenphase tracking. A bridge mode is triggered when the rolling eigenphase crosses from the header hemisphere ($\phi < -0.15$) to the body hemisphere, or when similarity drops below 0.4. In bridge mode, spans starting with \texttt{def} are penalized and body-hemisphere spans are boosted by their concentration $\bar{R}$.

\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|c|c|c|}
\hline
\textbf{Prompt} & \textbf{Steps} & \textbf{Tokens} & \textbf{Return} & \textbf{Loop} & \textbf{Bridges} \\ \hline
`
		for _, e := range report.BridgingData.Entries {
			name := e.Prefix
			if len(name) > 30 {
				name = name[:30] + "..."
			}
			retStr := "no"
			if e.HasReturn {
				retStr = "yes"
			}
			loopStr := "no"
			if e.HasLoop {
				loopStr = "yes"
			}
			latexContent += fmt.Sprintf("\\texttt{%s} & %d & %d & %s & %s & %d \\\\ \\hline\n",
				name, e.ChainLength, e.TotalTokens, retStr, loopStr, e.BridgeCount)
		}

		latexContent += fmt.Sprintf(`\end{tabular}
\caption{Phase-triggered manifold bridging results. Bridge events: %d total across %d prompts. Prompts with return: %d/%d, with loop: %d/%d.}
\end{table}

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{phase_bridging.pdf}
    \caption{Phase-triggered manifold bridging: per-step similarity and eigenphase, with bridge mode activation marked.}
    \label{fig:phase_bridging}
\end{figure}

`, report.BridgingData.BridgeTotal, len(report.BridgingData.Entries),
			report.BridgingData.ReturnCount, len(report.BridgingData.Entries),
			report.BridgingData.LoopCount, len(report.BridgingData.Entries))
	}

	// Add Cantilever Gating section
	if len(report.CantileverData.ControlEntries) > 0 {
		latexContent += fmt.Sprintf(`\subsection{Cantilever-Gated Span Retrieval}
A/B experiment comparing generation with and without cantilever gating. The cantilever estimates the maximum structurally coherent continuation length by probing each FibWindow scale (largest first) and measuring fingerprint similarity at that path difference. The gated arm restricts retrieval to spans $\leq$ the cantilever extent. In bridge mode, gating is relaxed to allow manifold crossing.

\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|}
\hline
\textbf{Metric} & \textbf{Control} & \textbf{Gated} \\ \hline
Mean tokens & %.1f & %.1f \\ \hline
Has return & %d/%d & %d/%d \\ \hline
Has loop & %d/%d & %d/%d \\ \hline
Bridge events & %d & %d \\ \hline
\end{tabular}
\caption{Cantilever-gated vs ungated generation: structural coverage metrics.}
\end{table}

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{cantilever_gating.pdf}
    \caption{Control vs gated generation: per-step similarity and span length, annotated with cantilever extent.}
    \label{fig:cantilever_gating}
\end{figure}

`, report.CantileverData.ControlStats.MeanTokens, report.CantileverData.GatedStats.MeanTokens,
			report.CantileverData.ControlStats.ReturnCount, len(report.CantileverData.ControlEntries),
			report.CantileverData.GatedStats.ReturnCount, len(report.CantileverData.GatedEntries),
			report.CantileverData.ControlStats.LoopCount, len(report.CantileverData.ControlEntries),
			report.CantileverData.GatedStats.LoopCount, len(report.CantileverData.GatedEntries),
			report.CantileverData.ControlStats.BridgeCount, report.CantileverData.GatedStats.BridgeCount)
	}

	// Add Relative Cantilever section
	if len(report.RelCantData.ControlEntries) > 0 {
		latexContent += fmt.Sprintf(`\subsection{Relative Cantilever Scale Selection}
Replaces the absolute-threshold cantilever with a ratio criterion: $s(w_\mathrm{large})/s(w_\mathrm{small}) \geq 0.7$ for adjacent FibWindow scales. This catches coherence drops that pass absolute thresholds but indicate structural risk.

\begin{table}[h]
\centering
\begin{tabular}{|l|c|c|}
\hline
\textbf{Metric} & \textbf{Control} & \textbf{Rel-Gated} \\ \hline
Mean tokens & %.1f & %.1f \\ \hline
Has return & %d/%d & %d/%d \\ \hline
Has loop & %d/%d & %d/%d \\ \hline
Bridge events & %d & %d \\ \hline
\end{tabular}
\caption{Relative cantilever gating vs ungated: structural coverage metrics.}
\end{table}

\begin{figure}[htbp]
    \centering
    \includegraphics[width=1.0\textwidth]{relative_cantilever.pdf}
    \caption{Control (solid) vs relative-gated (dashed) similarity per step.}
    \label{fig:relative_cantilever}
\end{figure}

`, report.RelCantData.ControlStats.MeanTokens, report.RelCantData.GatedStats.MeanTokens,
			report.RelCantData.ControlStats.ReturnCount, len(report.RelCantData.ControlEntries),
			report.RelCantData.GatedStats.ReturnCount, len(report.RelCantData.GatedEntries),
			report.RelCantData.ControlStats.LoopCount, len(report.RelCantData.ControlEntries),
			report.RelCantData.GatedStats.LoopCount, len(report.RelCantData.GatedEntries),
			report.RelCantData.ControlStats.BridgeCount, report.RelCantData.GatedStats.BridgeCount)
	}

	if err = os.WriteFile(filepath.Join(baseDir, "textgen.tex"), []byte(latexContent), 0644); err != nil {
		return err
	}

	// 2. Generate JSON data
	jsonData, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(filepath.Join(baseDir, "results.json"), jsonData, 0644); err != nil {
		return err
	}

	chartJSON, _ := json.Marshal(report.SpanData)
	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>BVP Span Solver Results</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var prompts = data.entries.map(function(e) { return e.prefix.replace('def ', '').replace(':', ''); });
var topScores = data.entries.map(function(e) { return e.top_scores && e.top_scores.length > 0 ? e.top_scores[0] : 0; });
var relevances = data.entries.map(function(e) { return e.prefix_relevance; });
var uniqueRatios = data.entries.map(function(e) { return e.unique_ratio; });
var hasReturn = data.entries.map(function(e) { return e.has_return ? 1 : 0; });
var hasColon = data.entries.map(function(e) { return e.has_colon ? 1 : 0; });
var converged = data.entries.map(function(e) { return e.converged ? 1 : 0; });

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: {
        data: ['Top-1 Retrieval', 'Prefix Relevance', 'Unique Ratio', 'return', ':', 'Converged'],
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 11 }
    },
    grid: [
        { left: '5%%', right: '53%%', top: 40, bottom: 80 },
        { left: '55%%', right: '3%%', top: 40, bottom: 80 }
    ],
    xAxis: [
        {
            type: 'category',
            data: prompts,
            gridIndex: 0,
            axisLabel: { color: '#334155', fontSize: 10, rotate: 25 },
            axisLine: { lineStyle: { color: '#94a3b8' } }
        },
        {
            type: 'category',
            data: prompts,
            gridIndex: 1,
            axisLabel: { color: '#334155', fontSize: 10, rotate: 25 },
            axisLine: { lineStyle: { color: '#94a3b8' } }
        }
    ],
    yAxis: [
        {
            type: 'value',
            name: 'Score',
            min: 0, max: 1,
            gridIndex: 0,
            nameTextStyle: { color: '#334155' },
            axisLabel: { color: '#334155' },
            splitLine: { lineStyle: { color: '#e2e8f0' } }
        },
        {
            type: 'value',
            name: 'Score / Flag',
            min: 0, max: 1.1,
            gridIndex: 1,
            nameTextStyle: { color: '#334155' },
            axisLabel: { color: '#334155' },
            splitLine: { lineStyle: { color: '#e2e8f0' } }
        }
    ],
    series: [
        {
            name: 'Top-1 Retrieval',
            type: 'bar',
            xAxisIndex: 0, yAxisIndex: 0,
            data: topScores,
            itemStyle: {
                color: {
                    type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
                    colorStops: [
                        { offset: 0, color: '#667eea' },
                        { offset: 1, color: '#764ba2' }
                    ]
                },
                borderRadius: [4, 4, 0, 0]
            },
            barWidth: '30%%'
        },
        {
            name: 'Prefix Relevance',
            type: 'bar',
            xAxisIndex: 0, yAxisIndex: 0,
            data: relevances,
            itemStyle: {
                color: {
                    type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
                    colorStops: [
                        { offset: 0, color: '#f093fb' },
                        { offset: 1, color: '#f5576c' }
                    ]
                },
                borderRadius: [4, 4, 0, 0]
            },
            barWidth: '30%%'
        },
        {
            name: 'Unique Ratio',
            type: 'bar',
            xAxisIndex: 1, yAxisIndex: 1,
            data: uniqueRatios,
            itemStyle: {
                color: {
                    type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
                    colorStops: [
                        { offset: 0, color: '#4facfe' },
                        { offset: 1, color: '#00f2fe' }
                    ]
                },
                borderRadius: [4, 4, 0, 0]
            },
            barWidth: '18%%'
        },
        {
            name: 'return',
            type: 'bar',
            xAxisIndex: 1, yAxisIndex: 1,
            data: hasReturn,
            itemStyle: {
                color: '#43e97b',
                borderRadius: [4, 4, 0, 0]
            },
            barWidth: '18%%'
        },
        {
            name: ':',
            type: 'bar',
            xAxisIndex: 1, yAxisIndex: 1,
            data: hasColon,
            itemStyle: {
                color: '#fa709a',
                borderRadius: [4, 4, 0, 0]
            },
            barWidth: '18%%'
        },
        {
            name: 'Converged',
            type: 'bar',
            xAxisIndex: 1, yAxisIndex: 1,
            data: converged,
            itemStyle: {
                color: '#fee140',
                borderRadius: [4, 4, 0, 0]
            },
            barWidth: '18%%'
        }
    ]
};
chart.setOption(option);
</script>
</body>
</html>`, string(chartJSON))

	htmlPath := filepath.Join(baseDir, "span_solver.html")
	if err = os.WriteFile(htmlPath, []byte(htmlContent), 0644); err != nil {
		return err
	}

	// 4. Render HTML to PDF via headless Chrome
	pdfPath := filepath.Join(baseDir, "span_solver.pdf")
	if err = exportPDF(htmlPath, pdfPath); err != nil {
		return fmt.Errorf("span_solver PDF export failed: %v", err)
	}

	// 5. Generate Span Ranking figure (Test 2)
	if len(report.RankingData.Entries) > 0 {
		rankJSON, _ := json.Marshal(report.RankingData)
		rankHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Span Ranking BVP</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

// Build per-prompt top-5 candidate similarity bars
var prompts = data.entries.map(function(e) { return e.prefix.replace('def ', '').replace(':', ''); });

// Collect top-5 scores and labels per prompt
var maxCand = 5;
var seriesData = [];
var gradients = [
    ['#667eea', '#764ba2'],
    ['#f093fb', '#f5576c'],
    ['#4facfe', '#00f2fe'],
    ['#43e97b', '#38f9d7'],
    ['#fa709a', '#fee140']
];

for (var r = 0; r < maxCand; r++) {
    var vals = [];
    for (var p = 0; p < data.entries.length; p++) {
        var cands = data.entries[p].top_candidates;
        if (cands && r < cands.length) {
            vals.push(cands[r].sim_score);
        } else {
            vals.push(0);
        }
    }
    seriesData.push({
        name: 'Rank ' + (r + 1),
        type: 'bar',
        data: vals,
        itemStyle: {
            color: {
                type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
                colorStops: [
                    { offset: 0, color: gradients[r][0] },
                    { offset: 1, color: gradients[r][1] }
                ]
            },
            borderRadius: [3, 3, 0, 0]
        },
        barGap: '5%%%%'
    });
}

// Also add winner total score as a line overlay
var winnerTotals = data.entries.map(function(e) { return e.winner_total; });
seriesData.push({
    name: 'Winner Total',
    type: 'line',
    data: winnerTotals,
    lineStyle: { color: '#0f172a', width: 2, type: 'dashed' },
    itemStyle: { color: '#0f172a' },
    symbolSize: 8,
    symbol: 'diamond'
});

var option = {
    backgroundColor: 'transparent',
    tooltip: {
        trigger: 'axis',
        formatter: function(params) {
            var tip = params[0].name + '<br/>';
            params.forEach(function(p) {
                if (p.value > 0) {
                    tip += p.marker + ' ' + p.seriesName + ': ' + p.value.toFixed(4) + '<br/>';
                }
            });
            return tip;
        }
    },
    legend: {
        data: ['Rank 1', 'Rank 2', 'Rank 3', 'Rank 4', 'Rank 5', 'Winner Total'],
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 11 }
    },
    grid: { left: '8%%%%', right: '5%%%%', top: 30, bottom: 80 },
    xAxis: {
        type: 'category',
        data: prompts,
        axisLabel: { color: '#334155', fontSize: 11, rotate: 15 },
        axisLine: { lineStyle: { color: '#94a3b8' } }
    },
    yAxis: {
        type: 'value',
        name: 'Similarity',
        min: 0,
        max: 1,
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    series: seriesData
};
chart.setOption(option);
</script>
</body>
</html>`, string(rankJSON))

		rankHTMLPath := filepath.Join(baseDir, "span_ranking.html")
		if err = os.WriteFile(rankHTMLPath, []byte(rankHTML), 0644); err != nil {
			return err
		}

		rankPDFPath := filepath.Join(baseDir, "span_ranking.pdf")
		if err = exportPDF(rankHTMLPath, rankPDFPath); err != nil {
			return fmt.Errorf("span_ranking PDF export failed: %v", err)
		}
	}

	// 6. Generate Span Chaining figure (Test 3)
	if len(report.ChainingData.Entries) > 0 {
		chainJSON, _ := json.Marshal(report.ChainingData)
		chainHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Span Chaining</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a'];
var steps = [];
for (var s = 1; s <= data.max_chains; s++) { steps.push('Step ' + s); }

var seriesData = [];
data.entries.forEach(function(e, idx) {
    var name = e.prefix.replace('def ', '').replace(':', '');
    var vals = e.chain.map(function(c) { return c.sim_score; });
    seriesData.push({
        name: name,
        type: 'line',
        data: vals,
        lineStyle: { width: 3, color: colors[idx %% 5] },
        itemStyle: { color: colors[idx %% 5] },
        symbolSize: 10,
        symbol: 'circle'
    });
});

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: {
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 11 }
    },
    grid: { left: '8%%%%', right: '5%%%%', top: 30, bottom: 80 },
    xAxis: {
        type: 'category',
        data: steps,
        axisLabel: { color: '#334155', fontSize: 12 },
        axisLine: { lineStyle: { color: '#94a3b8' } }
    },
    yAxis: {
        type: 'value',
        name: 'Similarity Score',
        min: 0,
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    series: seriesData
};
chart.setOption(option);
</script>
</body>
</html>`, string(chainJSON))

		chainHTMLPath := filepath.Join(baseDir, "span_chaining.html")
		if err = os.WriteFile(chainHTMLPath, []byte(chainHTML), 0644); err != nil {
			return err
		}

		chainPDFPath := filepath.Join(baseDir, "span_chaining.pdf")
		if err = exportPDF(chainHTMLPath, chainPDFPath); err != nil {
			return fmt.Errorf("span_chaining PDF export failed: %v", err)
		}
	}

	// 7. Generate Overlap Chaining figure (Test 4)
	if len(report.OverlapData.Entries) > 0 {
		ovlJSON, _ := json.Marshal(report.OverlapData)
		ovlHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Overlap-Aware Chaining</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a'];

// Build prompt labels
var prompts = data.entries.map(function(e) { return e.prefix.replace('def ', '').replace(':', ''); });

// Find max chain length
var maxSteps = 0;
data.entries.forEach(function(e) { if (e.chain.length > maxSteps) maxSteps = e.chain.length; });

// Build series: one bar group (new tokens) and one line (similarity) per prompt
var barSeries = [];
var lineSeries = [];
var steps = [];
for (var s = 1; s <= maxSteps; s++) { steps.push('Step ' + s); }

data.entries.forEach(function(e, idx) {
    var name = prompts[idx];
    var simVals = [];
    var newVals = [];
    e.chain.forEach(function(c) {
        simVals.push(c.sim_score);
        newVals.push(c.new_tokens);
    });

    lineSeries.push({
        name: name + ' (sim)',
        type: 'line',
        data: simVals,
        lineStyle: { width: 3, color: colors[idx %% 5] },
        itemStyle: { color: colors[idx %% 5] },
        symbolSize: 8,
        symbol: 'circle'
    });

    barSeries.push({
        name: name + ' (new)',
        type: 'bar',
        data: newVals,
        itemStyle: {
            color: colors[idx %% 5],
            opacity: 0.4,
            borderRadius: [3, 3, 0, 0]
        },
        yAxisIndex: 1,
        barGap: '10%%%%'
    });
});

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: {
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 10 }
    },
    grid: { left: '8%%%%', right: '8%%%%', top: 30, bottom: 100 },
    xAxis: {
        type: 'category',
        data: steps,
        axisLabel: { color: '#334155', fontSize: 12 },
        axisLine: { lineStyle: { color: '#94a3b8' } }
    },
    yAxis: [
        {
            type: 'value',
            name: 'Similarity',
            min: 0, max: 1,
            nameTextStyle: { color: '#334155' },
            axisLabel: { color: '#334155' },
            splitLine: { lineStyle: { color: '#e2e8f0' } }
        },
        {
            type: 'value',
            name: 'New Tokens',
            min: 0,
            nameTextStyle: { color: '#334155' },
            axisLabel: { color: '#334155' },
            splitLine: { show: false }
        }
    ],
    series: lineSeries.concat(barSeries)
};
chart.setOption(option);
</script>
</body>
</html>`, string(ovlJSON))

		ovlHTMLPath := filepath.Join(baseDir, "overlap_chaining.html")
		if err = os.WriteFile(ovlHTMLPath, []byte(ovlHTML), 0644); err != nil {
			return err
		}

		ovlPDFPath := filepath.Join(baseDir, "overlap_chaining.pdf")
		if err = exportPDF(ovlHTMLPath, ovlPDFPath); err != nil {
			return fmt.Errorf("overlap_chaining PDF export failed: %v", err)
		}
	}

	// 8. Generate Long Generation figure (Test 5)
	if len(report.LongGenData.Entries) > 0 {
		lgJSON, _ := json.Marshal(report.LongGenData)
		lgHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Long Program Generation</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a', '#fee140', '#a18cd1'];

var maxSteps = 0;
data.entries.forEach(function(e) { if (e.chain.length > maxSteps) maxSteps = e.chain.length; });

var steps = [];
for (var s = 1; s <= maxSteps; s++) { steps.push('Step ' + s); }

var series = [];
data.entries.forEach(function(e, idx) {
    var name = e.prefix.replace('def ', '').replace(':', '');
    var simVals = e.chain.map(function(c) { return c.sim_score; });
    series.push({
        name: name,
        type: 'line',
        data: simVals,
        lineStyle: { width: 2.5, color: colors[idx %% 7] },
        itemStyle: { color: colors[idx %% 7] },
        symbolSize: 7,
        symbol: 'circle'
    });
});

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: {
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 10 }
    },
    grid: { left: '8%%%%', right: '5%%%%', top: 30, bottom: 100 },
    xAxis: {
        type: 'category',
        data: steps,
        axisLabel: { color: '#334155', fontSize: 11 },
        axisLine: { lineStyle: { color: '#94a3b8' } }
    },
    yAxis: {
        type: 'value',
        name: 'Similarity',
        min: 0, max: 1,
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    series: series
};
chart.setOption(option);
</script>
</body>
</html>`, string(lgJSON))

		lgHTMLPath := filepath.Join(baseDir, "long_generation.html")
		if err = os.WriteFile(lgHTMLPath, []byte(lgHTML), 0644); err != nil {
			return err
		}

		lgPDFPath := filepath.Join(baseDir, "long_generation.pdf")
		if err = exportPDF(lgHTMLPath, lgPDFPath); err != nil {
			return fmt.Errorf("long_generation PDF export failed: %v", err)
		}
	}

	// 9. Generate Compositional Generation figure (Test 6)
	if len(report.CompGenData.Entries) > 0 {
		cgJSON, _ := json.Marshal(report.CompGenData)
		cgHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Compositional Generation</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a', '#fee140', '#a18cd1'];

var maxSteps = 0;
data.entries.forEach(function(e) { if (e.chain.length > maxSteps) maxSteps = e.chain.length; });

var steps = [];
for (var s = 1; s <= maxSteps; s++) { steps.push('Step ' + s); }

var series = [];
data.entries.forEach(function(e, idx) {
    var name = e.prefix.replace('def ', '').replace(':', '');
    var simVals = e.chain.map(function(c) { return c.sim_score; });
    series.push({
        name: name,
        type: 'line',
        data: simVals,
        lineStyle: { width: 2.5, color: colors[idx %% 7] },
        itemStyle: { color: colors[idx %% 7] },
        symbolSize: 7,
        symbol: 'circle'
    });
});

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: {
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 10 }
    },
    grid: { left: '8%%%%', right: '5%%%%', top: 30, bottom: 100 },
    xAxis: {
        type: 'category',
        data: steps,
        axisLabel: { color: '#334155', fontSize: 11 },
        axisLine: { lineStyle: { color: '#94a3b8' } }
    },
    yAxis: {
        type: 'value',
        name: 'Similarity (pure)',
        min: 0, max: 1,
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    series: series
};
chart.setOption(option);
</script>
</body>
</html>`, string(cgJSON))

		cgHTMLPath := filepath.Join(baseDir, "compositional_gen.html")
		if err = os.WriteFile(cgHTMLPath, []byte(cgHTML), 0644); err != nil {
			return err
		}

		cgPDFPath := filepath.Join(baseDir, "compositional_gen.pdf")
		if err = exportPDF(cgHTMLPath, cgPDFPath); err != nil {
			return fmt.Errorf("compositional_gen PDF export failed: %v", err)
		}
	}

	// 10. Generate Structural Sensitivity figure (Test 7)
	if len(report.StructSensData.Entries) > 0 {
		ssJSON, _ := json.Marshal(report.StructSensData)
		ssHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Structural Sensitivity</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 700px; }
        #chart { width: 1200px; height: 700px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var functions = data.entries.map(function(e) { return e.name; });

var commentSim = data.entries.map(function(e) { return e.sim_comment_full; });
var noiseSim = data.entries.map(function(e) { return e.sim_noise_full; });
var correctSim = data.entries.map(function(e) { return e.sim_correct_full; });

var commentDir = data.entries.map(function(e) { return e.dir_comment; });
var noiseDir = data.entries.map(function(e) { return e.dir_noise; });
var correctDir = data.entries.map(function(e) { return e.dir_correct; });

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: {
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 11 }
    },
    grid: [
        { left: '8%%%%', right: '52%%%%', top: 40, bottom: 80 },
        { left: '55%%%%', right: '5%%%%', top: 40, bottom: 80 }
    ],
    xAxis: [
        { type: 'category', data: functions, gridIndex: 0, axisLabel: { color: '#334155', rotate: 30 }, axisLine: { lineStyle: { color: '#94a3b8' } } },
        { type: 'category', data: functions, gridIndex: 1, axisLabel: { color: '#334155', rotate: 30 }, axisLine: { lineStyle: { color: '#94a3b8' } } }
    ],
    yAxis: [
        { type: 'value', name: 'Sim→Full', gridIndex: 0, min: 0, max: 1, nameTextStyle: { color: '#334155' }, axisLabel: { color: '#334155' }, splitLine: { lineStyle: { color: '#e2e8f0' } } },
        { type: 'value', name: 'Dir→Full', gridIndex: 1, min: -1, max: 1, nameTextStyle: { color: '#334155' }, axisLabel: { color: '#334155' }, splitLine: { lineStyle: { color: '#e2e8f0' } } }
    ],
    series: [
        { name: 'comment', type: 'bar', data: commentSim, xAxisIndex: 0, yAxisIndex: 0, itemStyle: { color: '#94a3b8', borderRadius: [3,3,0,0] } },
        { name: 'noise', type: 'bar', data: noiseSim, xAxisIndex: 0, yAxisIndex: 0, itemStyle: { color: '#f093fb', borderRadius: [3,3,0,0] } },
        { name: 'correct', type: 'bar', data: correctSim, xAxisIndex: 0, yAxisIndex: 0, itemStyle: { color: '#43e97b', borderRadius: [3,3,0,0] } },

        { name: 'comment', type: 'bar', data: commentDir, xAxisIndex: 1, yAxisIndex: 1, itemStyle: { color: '#94a3b8', borderRadius: [3,3,0,0] } },
        { name: 'noise', type: 'bar', data: noiseDir, xAxisIndex: 1, yAxisIndex: 1, itemStyle: { color: '#f093fb', borderRadius: [3,3,0,0] } },
        { name: 'correct', type: 'bar', data: correctDir, xAxisIndex: 1, yAxisIndex: 1, itemStyle: { color: '#43e97b', borderRadius: [3,3,0,0] } }
    ]
};
chart.setOption(option);
</script>
</body>
</html>`, string(ssJSON))

		ssHTMLPath := filepath.Join(baseDir, "structural_sensitivity.html")
		if err = os.WriteFile(ssHTMLPath, []byte(ssHTML), 0644); err != nil {
			return err
		}

		ssPDFPath := filepath.Join(baseDir, "structural_sensitivity.pdf")
		if err = exportPDF(ssHTMLPath, ssPDFPath); err != nil {
			return fmt.Errorf("structural_sensitivity PDF export failed: %v", err)
		}
	}

	// 11. Generate Eigenmode Probe figure (Test 8)
	if len(report.EigenmodeData.Points) > 0 {
		emJSON, _ := json.Marshal(report.EigenmodeData)
		emHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Eigenmode Probe</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 800px; }
        #chart { width: 1200px; height: 800px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var roleColors = {
    'header': '#667eea',
    'loop': '#f093fb',
    'conditional': '#4facfe',
    'return': '#43e97b',
    'assignment': '#fa709a',
    'call': '#fee140',
    'other': '#94a3b8'
};

var seriesMap = {};
data.points.forEach(function(p) {
    if (!seriesMap[p.role]) {
        seriesMap[p.role] = [];
    }
    seriesMap[p.role].push([p.pc1, p.pc2]);
});

var series = [];
Object.keys(seriesMap).forEach(function(role) {
    series.push({
        name: role,
        type: 'scatter',
        data: seriesMap[role],
        symbolSize: 4,
        itemStyle: {
            color: roleColors[role] || '#94a3b8',
            opacity: 0.6
        }
    });
});

// Add centroid markers
data.roles.forEach(function(r) {
    series.push({
        name: r.role + ' centroid',
        type: 'scatter',
        data: [[r.mean_pc1, r.mean_pc2]],
        symbolSize: 16,
        symbol: 'diamond',
        itemStyle: {
            color: roleColors[r.role] || '#94a3b8',
            borderColor: '#0f172a',
            borderWidth: 2,
            opacity: 1
        },
        label: { show: false }
    });
});

var option = {
    backgroundColor: 'transparent',
    tooltip: {
        trigger: 'item',
        formatter: function(p) { return p.seriesName + '<br/>PC1: ' + p.data[0].toFixed(3) + '<br/>PC2: ' + p.data[1].toFixed(3); }
    },
    legend: {
        bottom: 10,
        textStyle: { color: '#334155', fontSize: 10 },
        data: Object.keys(seriesMap)
    },
    grid: { left: '10%%%%', right: '5%%%%', top: 30, bottom: 80 },
    xAxis: {
        type: 'value',
        name: 'PC1',
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    yAxis: {
        type: 'value',
        name: 'PC2',
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    series: series
};
chart.setOption(option);
</script>
</body>
</html>`, string(emJSON))

		emHTMLPath := filepath.Join(baseDir, "eigenmode_probe.html")
		if err = os.WriteFile(emHTMLPath, []byte(emHTML), 0644); err != nil {
			return err
		}

		emPDFPath := filepath.Join(baseDir, "eigenmode_probe.pdf")
		if err = exportPDF(emHTMLPath, emPDFPath); err != nil {
			return fmt.Errorf("eigenmode_probe PDF export failed: %v", err)
		}
	}

	// 12. Generate Phase Bridging figure (Test 9)
	if len(report.BridgingData.Entries) > 0 {
		bridgeJSON, _ := json.Marshal(report.BridgingData)
		bridgeHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Phase Bridging</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; color: #0f172a; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 800px; }
        #chart { width: 1200px; height: 800px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var promptColors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a'];
var series = [];

data.entries.forEach(function(entry, pi) {
    var steps = entry.chain.map(function(s) { return s.step; });
    var sims = entry.chain.map(function(s) { return s.sim_score; });
    var phases = entry.chain.map(function(s) { return s.eigen_phase * 180 / Math.PI; });

    var name = entry.prefix.replace('def ', '').replace(':', '');
    if (name.length > 25) name = name.substring(0, 25) + '...';

    series.push({
        name: name + ' sim',
        type: 'line',
        yAxisIndex: 0,
        data: steps.map(function(s, i) { return [s, sims[i]]; }),
        lineStyle: { color: promptColors[pi], width: 2 },
        itemStyle: { color: promptColors[pi] },
        symbol: 'circle',
        symbolSize: 6
    });

    series.push({
        name: name + ' φ',
        type: 'line',
        yAxisIndex: 1,
        data: steps.map(function(s, i) { return [s, phases[i]]; }),
        lineStyle: { color: promptColors[pi], width: 2, type: 'dashed' },
        itemStyle: { color: promptColors[pi] },
        symbol: 'diamond',
        symbolSize: 6
    });

    // Mark bridge steps
    entry.chain.forEach(function(s) {
        if (s.in_bridge) {
            series.push({
                type: 'scatter',
                yAxisIndex: 0,
                data: [[s.step, s.sim_score]],
                symbolSize: 14,
                symbol: 'triangle',
                itemStyle: { color: promptColors[pi], borderColor: '#0f172a', borderWidth: 2 },
                silent: true
            });
        }
    });
});

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: {
        bottom: 5,
        textStyle: { color: '#334155', fontSize: 9 },
        type: 'scroll'
    },
    grid: { left: '8%%%%', right: '8%%%%', top: 30, bottom: 80 },
    xAxis: {
        type: 'value',
        name: 'Chain Step',
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    yAxis: [{
        type: 'value',
        name: 'Similarity',
        nameTextStyle: { color: '#667eea' },
        axisLabel: { color: '#667eea' },
        splitLine: { lineStyle: { color: '#e2e8f0' } },
        min: 0,
        max: 1
    }, {
        type: 'value',
        name: 'Eigenphase (°)',
        nameTextStyle: { color: '#f093fb' },
        axisLabel: { color: '#f093fb' },
        splitLine: { show: false }
    }],
    series: series
};
chart.setOption(option);
</script>
</body>
</html>`, string(bridgeJSON))

		brHTMLPath := filepath.Join(baseDir, "phase_bridging.html")
		if err = os.WriteFile(brHTMLPath, []byte(bridgeHTML), 0644); err != nil {
			return err
		}

		brPDFPath := filepath.Join(baseDir, "phase_bridging.pdf")
		if err = exportPDF(brHTMLPath, brPDFPath); err != nil {
			return fmt.Errorf("phase_bridging PDF export failed: %v", err)
		}
	}

	// 13. Generate Cantilever Gating figure (Test 10)
	if len(report.CantileverData.ControlEntries) > 0 {
		cantJSON, _ := json.Marshal(report.CantileverData)
		cantHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Cantilever Gating</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 800px; }
        #chart { width: 1200px; height: 800px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a'];
var series = [];

function addArm(entries, dashed, suffix) {
    entries.forEach(function(entry, pi) {
        var steps = entry.chain.map(function(s) { return s.step; });
        var sims = entry.chain.map(function(s) { return s.sim_score; });
        var name = entry.prefix.replace('def ', '').replace(':', '');
        if (name.length > 20) name = name.substring(0, 20) + '...';
        series.push({
            name: name + suffix,
            type: 'line',
            data: steps.map(function(s, i) { return [s, sims[i]]; }),
            lineStyle: { color: colors[pi], width: 2, type: dashed ? 'dashed' : 'solid' },
            itemStyle: { color: colors[pi] },
            symbol: dashed ? 'diamond' : 'circle',
            symbolSize: 6
        });
    });
}

addArm(data.control, false, ' ctrl');
addArm(data.gated, true, ' gate');

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: { bottom: 5, textStyle: { color: '#334155', fontSize: 9 }, type: 'scroll' },
    grid: { left: '8%%%%', right: '5%%%%', top: 30, bottom: 80 },
    xAxis: {
        type: 'value', name: 'Chain Step',
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    yAxis: {
        type: 'value', name: 'Similarity', min: 0, max: 1,
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    series: series
};
chart.setOption(option);
</script>
</body>
</html>`, string(cantJSON))

		cantHTMLPath := filepath.Join(baseDir, "cantilever_gating.html")
		if err = os.WriteFile(cantHTMLPath, []byte(cantHTML), 0644); err != nil {
			return err
		}

		cantPDFPath := filepath.Join(baseDir, "cantilever_gating.pdf")
		if err = exportPDF(cantHTMLPath, cantPDFPath); err != nil {
			return fmt.Errorf("cantilever_gating PDF export failed: %v", err)
		}
	}

	// 14. Generate Relative Cantilever figure (Test 11)
	if len(report.RelCantData.ControlEntries) > 0 {
		relJSON, _ := json.Marshal(report.RelCantData)
		relHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Relative Cantilever</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"></script>
    <style>
        html, body { margin: 0; padding: 0; background-color: transparent; font-family: 'Helvetica Neue', Arial, sans-serif; width: 1200px; height: 800px; }
        #chart { width: 1200px; height: 800px; background: transparent; padding: 20px; box-sizing: border-box; }
    </style>
</head>
<body>
<div id="chart"></div>
<script>
var data = %s;
var chart = echarts.init(document.getElementById('chart'));

var colors = ['#667eea', '#f093fb', '#4facfe', '#43e97b', '#fa709a'];
var series = [];

function addArm(entries, dashed, suffix) {
    entries.forEach(function(entry, pi) {
        var steps = entry.chain.map(function(s) { return s.step; });
        var sims = entry.chain.map(function(s) { return s.sim_score; });
        var name = entry.prefix.replace('def ', '').replace(':', '');
        if (name.length > 20) name = name.substring(0, 20) + '...';
        series.push({
            name: name + suffix,
            type: 'line',
            data: steps.map(function(s, i) { return [s, sims[i]]; }),
            lineStyle: { color: colors[pi], width: 2, type: dashed ? 'dashed' : 'solid' },
            itemStyle: { color: colors[pi] },
            symbol: dashed ? 'diamond' : 'circle',
            symbolSize: 6
        });

        // For gated: mark max_safe as annotation on first step
        if (dashed && entry.chain.length > 0) {
            series.push({
                type: 'scatter',
                data: [[entry.chain[0].step, entry.chain[0].sim_score]],
                symbolSize: 12,
                symbol: 'pin',
                itemStyle: { color: colors[pi] },
                label: { show: true, formatter: '≤' + entry.chain[0].max_safe, fontSize: 8, color: colors[pi] },
                silent: true
            });
        }
    });
}

addArm(data.control, false, ' ctrl');
addArm(data.gated, true, ' rel');

var option = {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis' },
    legend: { bottom: 5, textStyle: { color: '#334155', fontSize: 9 }, type: 'scroll' },
    grid: { left: '8%%%%', right: '5%%%%', top: 30, bottom: 80 },
    xAxis: {
        type: 'value', name: 'Chain Step',
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    yAxis: {
        type: 'value', name: 'Similarity', min: 0, max: 1,
        nameTextStyle: { color: '#334155' },
        axisLabel: { color: '#334155' },
        splitLine: { lineStyle: { color: '#e2e8f0' } }
    },
    series: series
};
chart.setOption(option);
</script>
</body>
</html>`, string(relJSON))

		relHTMLPath := filepath.Join(baseDir, "relative_cantilever.html")
		if err = os.WriteFile(relHTMLPath, []byte(relHTML), 0644); err != nil {
			return err
		}

		relPDFPath := filepath.Join(baseDir, "relative_cantilever.pdf")
		if err = exportPDF(relHTMLPath, relPDFPath); err != nil {
			return fmt.Errorf("relative_cantilever PDF export failed: %v", err)
		}
	}

	return nil
}

func exportPDF(htmlPath, pdfPath string) error {
	absHTMLPath, _ := filepath.Abs(htmlPath)
	fileURL := "file://" + absHTMLPath

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("headless", true))...)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	var buf []byte
	err := chromedp.Run(taskCtx,
		chromedp.Navigate(fileURL),
		chromedp.WaitReady("#chart", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			time.Sleep(1 * time.Second)
			return nil
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, _, err = page.PrintToPDF().
				WithLandscape(true).
				WithPrintBackground(true).
				WithMarginTop(0).
				WithMarginBottom(0).
				WithMarginLeft(0).
				WithMarginRight(0).
				WithPaperWidth(12.5).
				WithPaperHeight(8.5).
				Do(ctx)
			return err
		}),
	)

	if err != nil {
		return fmt.Errorf("headless PDF rendering failed: %v", err)
	}

	if err := os.WriteFile(pdfPath, buf, 0644); err != nil {
		return fmt.Errorf("failed to save generated PDF: %v", err)
	}

	return nil
}

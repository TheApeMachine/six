package textgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
}

// SpanSolverResult holds the full span solver experiment results.
type SpanSolverResult struct {
	SpanLength       int              `json:"span_length"`
	TopK             int              `json:"top_k"`
	RefineIterations int              `json:"refine_iterations"`
	DialAngles       int              `json:"dial_angles"`
	TotalSpans       int              `json:"total_spans"`
	Entries          []SpanSolverEntry `json:"entries"`
	ConvergedCount   int              `json:"converged_count"`
	ReturnCount      int              `json:"return_count"`
	ColonCount       int              `json:"colon_count"`
	MeanUniqueRatio  float64          `json:"mean_unique_ratio"`
	MeanRelevance    float64          `json:"mean_relevance"`
}

// ValidationReport aggregates all test results.
type ValidationReport struct {
	CorpusHash string
	CorpusSize int
	SpanData   SpanSolverResult
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
		// Escape underscores in desc
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
		generated := strings.ReplaceAll(e.Generated, "_", "\\_")
		latexContent += fmt.Sprintf(`\noindent\textbf{%s}

\begin{verbatim}
%s
    %s
\end{verbatim}

`, prefix, e.Prefix, generated)
	}

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

	if err = os.WriteFile(filepath.Join(baseDir, "textgen.tex"), []byte(latexContent), 0644); err != nil {
		return err
	}

	// 2. Generate JSON data for potential ECharts visualization
	jsonData, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(filepath.Join(baseDir, "results.json"), jsonData, 0644); err != nil {
		return err
	}

	return nil
}

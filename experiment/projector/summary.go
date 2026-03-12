package projector

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tools "github.com/theapemachine/six/experiment"
)

/*
RunTiming holds the wall-clock breakdown captured by the Pipeline for each
experimental run.  All fields are optional — a zero Duration is simply
omitted from the rendered table.
*/
type RunTiming struct {
	LoadDur     time.Duration
	PromptDur   time.Duration
	FinalizeDur time.Duration
	N           int // number of prompts processed
}

// maxSampleBytes is the max bytes shown for any prefix/holdout/observed cell.
const maxSampleBytes = 40

// SummaryConfig controls how many rows to show in each section.
type SummaryConfig struct {
	TopN    int // best-scoring rows to show
	BottomN int // worst-scoring rows to show
	SampN   int // generation example pairs to show (best + worst)
}

/*
DefaultSummaryConfig returns the standard table layout used across all experiments.
*/
func DefaultSummaryConfig() SummaryConfig {
	return SummaryConfig{TopN: 3, BottomN: 3, SampN: 2}
}

/*
WriteSummaryTable generates a standardized three-section LaTeX table for an experiment:

  - Top N results (best-scoring)
  - Bottom N results (worst-scoring, only when distinct from top)
  - Generation examples (curated prefix → expected → observed)
  - Conditions block (N, mean, min, max, holdout chars, score weights)
*/
func WriteSummaryTable(
	name, section string,
	rows []tools.ExperimentalData,
	holdoutN int,
	holdoutType string,
	cfg SummaryConfig,
	timing RunTiming,
	outDir, outFile string,
) error {
	if len(rows) == 0 {
		return nil
	}

	// Sort a copy descending by weighted score.
	sorted := make([]tools.ExperimentalData, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].WeightedTotal > sorted[j].WeightedTotal
	})

	topN := min(cfg.TopN, len(sorted))
	top := sorted[:topN]

	// Bottom rows — skip any already in top set.
	topSet := make(map[int]bool, topN)
	for _, r := range top {
		topSet[r.Idx] = true
	}

	var bottom []tools.ExperimentalData
	for i := len(sorted) - 1; i >= 0 && len(bottom) < cfg.BottomN; i-- {
		if !topSet[sorted[i].Idx] {
			bottom = append(bottom, sorted[i])
		}
	}

	// Generation examples: up to SampN from top and SampN from bottom
	// that actually have Prefix/Holdout/Observed data.
	var genRows []tools.ExperimentalData
	hasGen := func(r tools.ExperimentalData) bool {
		return len(r.Holdout) > 0 || len(r.Observed) > 0
	}

	added := make(map[int]bool)
	for _, r := range top {
		if len(genRows) >= cfg.SampN {
			break
		}
		if hasGen(r) && !added[r.Idx] {
			genRows = append(genRows, r)
			added[r.Idx] = true
		}
	}
	for i := len(sorted) - 1; i >= 0 && len(genRows) < cfg.SampN*2; i-- {
		r := sorted[i]
		if hasGen(r) && !added[r.Idx] {
			genRows = append(genRows, r)
			added[r.Idx] = true
		}
	}

	// Compute summary stats.
	total, minS, maxS := 0.0, math.MaxFloat64, -math.MaxFloat64
	for _, r := range rows {
		total += r.WeightedTotal
		if r.WeightedTotal < minS {
			minS = r.WeightedTotal
		}
		if r.WeightedTotal > maxS {
			maxS = r.WeightedTotal
		}
	}
	mean := total / float64(len(rows))

	var sb strings.Builder

	sb.WriteString("\\begin{table}[htbp]\n")
	sb.WriteString("\\centering\n")
	sb.WriteString(fmt.Sprintf("\\caption{%s — standardised result summary (N=%d).}\n",
		LaTeXEscape(name), len(rows)))
	sb.WriteString(fmt.Sprintf("\\label{tab:%s_summary}\n", tools.Slugify(name)))
	sb.WriteString("\\begin{adjustbox}{max width=\\textwidth}\n")
	sb.WriteString("\\begin{tabular}{r p{5cm} r r r r}\n")
	sb.WriteString("\\toprule\n")

	// ── Top results ──────────────────────────────────────────────────────────
	sb.WriteString(fmt.Sprintf(
		"\\multicolumn{6}{l}{\\textbf{Top %d results (highest weighted score)}} \\\\\n", topN))
	sb.WriteString("\\midrule\n")
	sb.WriteString("\\# & Name & Exact & Partial & Fuzzy & Weighted \\\\\n")
	sb.WriteString("\\midrule\n")
	for _, r := range top {
		sb.WriteString(fmt.Sprintf("%d & %s & %.4f & %.4f & %.4f & %.4f \\\\\n",
			r.Idx, LaTeXEscape(r.Name),
			r.Scores.Exact, r.Scores.Partial, r.Scores.Fuzzy, r.WeightedTotal))
	}

	// ── Bottom results (only if distinct) ────────────────────────────────────
	if len(bottom) > 0 {
		sb.WriteString("\\midrule\n")
		sb.WriteString(fmt.Sprintf(
			"\\multicolumn{6}{l}{\\textbf{Bottom %d results (lowest weighted score)}} \\\\\n", len(bottom)))
		sb.WriteString("\\midrule\n")
		sb.WriteString("\\# & Name & Exact & Partial & Fuzzy & Weighted \\\\\n")
		sb.WriteString("\\midrule\n")
		for _, r := range bottom {
			sb.WriteString(fmt.Sprintf("%d & %s & %.4f & %.4f & %.4f & %.4f \\\\\n",
				r.Idx, LaTeXEscape(r.Name),
				r.Scores.Exact, r.Scores.Partial, r.Scores.Fuzzy, r.WeightedTotal))
		}
	}

	// ── Generation examples ─────────────────────────────────────────────────────
	if len(genRows) > 0 {
		sb.WriteString("\\midrule\n")
		sb.WriteString(fmt.Sprintf(
			"\\multicolumn{6}{l}{\\textbf{Generation examples (%d curated)}} \\\\\n", len(genRows)))
		sb.WriteString("\\midrule\n")
		sb.WriteString("\\multicolumn{2}{l}{\\textit{Prefix (truncated)}} & \\multicolumn{2}{l}{\\textit{Expected}} & \\multicolumn{2}{l}{\\textit{Observed}} \\\\\n")
		sb.WriteString("\\midrule\n")
		for _, r := range genRows {
			// sampleCell: escape raw bytes → collapse newlines → truncate.
			// Order matters: LaTeXEscape must run on raw text; truncate must
			// add \ldots AFTER escaping so it is not re-escaped.
			prefix := sampleCell(r.Prefix)
			expected := sampleCell(r.Holdout)
			observed := sampleCell(r.Observed)
			sb.WriteString(fmt.Sprintf(
				"\\multicolumn{2}{l}{\\texttt{%s}} & \\multicolumn{2}{l}{\\texttt{%s}} & \\multicolumn{2}{l}{\\texttt{%s}} \\\\\n",
				prefix, expected, observed))
		}
	}

	// ── Conditions ───────────────────────────────────────────────────────────
	sb.WriteString("\\midrule\n")
	sb.WriteString("\\multicolumn{6}{l}{\\textbf{Experiment conditions}} \\\\\n")
	sb.WriteString("\\midrule\n")

	sb.WriteString(fmt.Sprintf(
		"\\multicolumn{3}{l}{Samples: %d} & \\multicolumn{3}{l}{Holdout: %d\\%% (%s)} \\\\\n",
		len(rows), holdoutN, holdoutType))
	sb.WriteString(fmt.Sprintf(
		"\\multicolumn{3}{l}{Mean score: %.4f} & \\multicolumn{3}{l}{Min: %.4f \\quad Max: %.4f} \\\\\n",
		mean, minS, maxS))
	sb.WriteString(fmt.Sprintf(
		"\\multicolumn{6}{l}{Score weights: Exact $\\times$1.0, Partial $\\times$0.5, Fuzzy $\\times$%.2f} \\\\\n",
		1.0/3.0))

	// ── Timing ───────────────────────────────────────────────────────────────
	wallTotal := timing.LoadDur + timing.PromptDur + timing.FinalizeDur
	if wallTotal > 0 {
		sb.WriteString("\\midrule\n")
		sb.WriteString("\\multicolumn{6}{l}{\\textbf{Timing}} \\\\\n")
		sb.WriteString("\\midrule\n")
		if timing.LoadDur > 0 {
			sb.WriteString(fmt.Sprintf(
				"\\multicolumn{3}{l}{Dataset load:} & \\multicolumn{3}{l}{%s} \\\\\n",
				fmtDur(timing.LoadDur)))
		}
		if timing.PromptDur > 0 {
			sb.WriteString(fmt.Sprintf(
				"\\multicolumn{3}{l}{Inference loop:} & \\multicolumn{3}{l}{%s} \\\\\n",
				fmtDur(timing.PromptDur)))
			if timing.N > 0 {
				meanPred := timing.PromptDur / time.Duration(timing.N)
				sb.WriteString(fmt.Sprintf(
					"\\multicolumn{3}{l}{Mean per prediction:} & \\multicolumn{3}{l}{%s} \\\\\n",
					fmtDur(meanPred)))
			}
		}
		if timing.FinalizeDur > 0 {
			sb.WriteString(fmt.Sprintf(
				"\\multicolumn{3}{l}{Finalize:} & \\multicolumn{3}{l}{%s} \\\\\n",
				fmtDur(timing.FinalizeDur)))
		}
		sb.WriteString(fmt.Sprintf(
			"\\multicolumn{3}{l}{\\textbf{Total wall-clock:}} & \\multicolumn{3}{l}{\\textbf{%s}} \\\\\n",
			fmtDur(wallTotal)))
	}

	sb.WriteString("\\bottomrule\n")
	sb.WriteString("\\end{tabular}\n")
	sb.WriteString("\\end{adjustbox}\n")
	sb.WriteString("\\end{table}\n")

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(outDir, outFile), []byte(sb.String()), 0644)
}

// sampleCell prepares a raw byte slice for safe embedding inside \texttt{}:
//  1. Sanitize: strip invalid UTF-8 sequences and non-printable control bytes.
//  2. Collapse newlines/tabs to spaces, trim.
//  3. Truncate by RUNE count BEFORE LaTeX escaping so we never cut through
//     a multi-byte rune or a multi-character escape sequence.
//  4. LaTeXEscape the truncated text.
//  5. Append \ldots if truncated.
func sampleCell(raw []byte) string {
	s := sanitizeForLaTeX(raw)
	s = strings.NewReplacer("\n", " ", "\r", "", "\t", " ").Replace(s)
	s = strings.TrimSpace(s)

	runes := []rune(s)
	truncated := len(runes) > maxSampleBytes

	if truncated {
		runes = runes[:maxSampleBytes]
	}

	s = LaTeXEscape(string(runes))

	if truncated {
		s += "\\ldots"
	}

	return s
}

// sanitizeForLaTeX converts an arbitrary byte slice to a valid UTF-8 string
// suitable for embedding in LaTeX source.  Invalid UTF-8 sequences and
// non-printable control runes (except ordinary space) are replaced with '·'
// so that the caller never writes 0xFF or similar into a .tex file.
func sanitizeForLaTeX(raw []byte) string {
	var sb strings.Builder
	sb.Grow(len(raw))

	for len(raw) > 0 {
		r, size := utf8.DecodeRune(raw)
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte — skip entirely.
			raw = raw[size:]
			continue
		}
		if r != ' ' && (unicode.IsControl(r) || !unicode.IsPrint(r)) {
			sb.WriteRune('·')
		} else {
			sb.WriteRune(r)
		}
		raw = raw[size:]
	}

	return sb.String()
}

// fmtDur formats a duration as a compact human-readable string.
func fmtDur(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%d\u00b5s", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.1f ms", float64(d.Milliseconds()))
	case d < time.Minute:
		return fmt.Sprintf("%.2f s", d.Seconds())
	default:
		return fmt.Sprintf("%.1f min", d.Minutes())
	}
}

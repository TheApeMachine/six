package logic

import (
	"fmt"
	"sort"
	"strings"

	tools "github.com/theapemachine/six/experiment"
)

/*
babiSummary aggregates the per-sample bAbI Task 1 rows into the compact
distributions that drive the four-panel result figure. The original
fingerprint heatmap is illegible at production sample counts (N≥500), so
this object derives the four orthogonal slices that actually carry signal:
outcome buckets, mean score components, per-location exact accuracy, and
the weighted-score histogram. Each panel is independently meaningful so the
figure communicates the result without leaning on the prose body.
*/
type babiSummary struct {
	rows []tools.ExperimentalData
}

func newBabiSummary(rows []tools.ExperimentalData) *babiSummary {
	return &babiSummary{rows: rows}
}

/*
Panels emits the four-panel layout. Order is deliberate: the top row carries
the headline (outcome share, score components) and the bottom row carries
the structural axes (location, weighted distribution).
*/
func (summary *babiSummary) Panels() []tools.Panel {
	if len(summary.rows) == 0 {
		return nil
	}

	return []tools.Panel{
		summary.outcomePanel(),
		summary.componentPanel(),
		summary.locationPanel(),
		summary.histogramPanel(),
	}
}

/*
outcomePanel slices every sample into Exact / Partial / Zero. Partial covers
"weighted > 0 but not exact" — i.e. the substrate produced location-shaped
residue but did not pin the right one. Zero is no-signal output. Reporting
this triple makes the distance between "wrong shape" and "near-miss" visible
at a glance, which a single accuracy number hides.
*/
func (summary *babiSummary) outcomePanel() tools.Panel {
	exact, partial, zero := 0, 0, 0

	for _, row := range summary.rows {
		switch {
		case row.Scores.Exact == 1.0:
			exact++
		case row.WeightedTotal > 0:
			partial++
		default:
			zero++
		}
	}

	total := float64(len(summary.rows))

	return tools.Panel{
		Kind:       "chart",
		Title:      "Outcome Breakdown",
		GridLeft:   "6%",
		GridRight:  "55%",
		GridTop:    "12%",
		GridBottom: "16%",
		XLabels:    []string{"Exact", "Partial", "Zero"},
		XAxisName:  "Outcome",
		XShow:      true,
		YAxisName:  "Share of samples",
		Series: []tools.PanelSeries{
			{
				Name:     "Share",
				Kind:     "bar",
				BarWidth: "55%",
				Color:    "#22c55e",
				Data: []float64{
					float64(exact) / total,
					float64(partial) / total,
					float64(zero) / total,
				},
			},
		},
		YMin: tools.Float64Ptr(0),
		YMax: tools.Float64Ptr(1),
	}
}

/*
componentPanel reports the mean Exact / Partial / Fuzzy / Weighted scores
across all samples. Exact is the headline number; the gap to Partial and
Fuzzy quantifies how often the substrate reaches the right neighbourhood
without locking onto the exact location token.
*/
func (summary *babiSummary) componentPanel() tools.Panel {
	var sumExact, sumPartial, sumFuzzy, sumWeighted float64

	for _, row := range summary.rows {
		sumExact += row.Scores.Exact
		sumPartial += row.Scores.Partial
		sumFuzzy += row.Scores.Fuzzy
		sumWeighted += row.WeightedTotal
	}

	count := float64(len(summary.rows))

	return tools.Panel{
		Kind:       "chart",
		Title:      "Mean Score Components",
		GridLeft:   "58%",
		GridRight:  "5%",
		GridTop:    "12%",
		GridBottom: "16%",
		XLabels:    []string{"Exact", "Partial", "Fuzzy", "Weighted"},
		XAxisName:  "Component",
		XShow:      true,
		YAxisName:  "Mean score",
		Series: []tools.PanelSeries{
			{
				Name:     "Mean",
				Kind:     "bar",
				BarWidth: "55%",
				Color:    "#3b82f6",
				Data: []float64{
					sumExact / count,
					sumPartial / count,
					sumFuzzy / count,
					sumWeighted / count,
				},
			},
		},
		YMin: tools.Float64Ptr(0),
		YMax: tools.Float64Ptr(1),
	}
}

/*
locationPanel groups samples by their expected location and reports exact
accuracy per location, sorted descending. Variance across locations is the
clearest tell that the substrate has location-specific attractor structure
rather than uniform random hits.
*/
func (summary *babiSummary) locationPanel() tools.Panel {
	type bucket struct {
		label string
		exact int
		total int
	}

	index := map[string]*bucket{}

	for _, row := range summary.rows {
		key := strings.TrimSpace(string(row.Holdout))
		if key == "" {
			continue
		}

		entry := index[key]
		if entry == nil {
			entry = &bucket{label: key}
			index[key] = entry
		}

		entry.total++
		if row.Scores.Exact == 1.0 {
			entry.exact++
		}
	}

	buckets := make([]*bucket, 0, len(index))
	for _, entry := range index {
		buckets = append(buckets, entry)
	}

	sort.Slice(buckets, func(left, right int) bool {
		leftAcc := float64(buckets[left].exact) / float64(buckets[left].total)
		rightAcc := float64(buckets[right].exact) / float64(buckets[right].total)
		return leftAcc > rightAcc
	})

	labels := make([]string, len(buckets))
	values := make([]float64, len(buckets))

	for idx, entry := range buckets {
		labels[idx] = fmt.Sprintf("%s (n=%d)", entry.label, entry.total)
		values[idx] = float64(entry.exact) / float64(entry.total)
	}

	return tools.Panel{
		Kind:       "chart",
		Title:      "Exact Accuracy by Location",
		GridLeft:   "6%",
		GridRight:  "55%",
		GridTop:    "55%",
		GridBottom: "8%",
		XLabels:    labels,
		XAxisName:  "Expected location",
		XShow:      true,
		YAxisName:  "Exact accuracy",
		Series: []tools.PanelSeries{
			{
				Name:     "Exact",
				Kind:     "bar",
				BarWidth: "55%",
				Color:    "#a855f7",
				Data:     values,
			},
		},
		YMin: tools.Float64Ptr(0),
		YMax: tools.Float64Ptr(1),
	}
}

/*
histogramPanel bins weighted scores into ten equal-width buckets across
[0, 1]. The bAbI weighted distribution is strongly tri-modal (zero-signal,
half-credit residue, exact match), and showing the histogram makes that
shape explicit instead of collapsing it into a mean.
*/
func (summary *babiSummary) histogramPanel() tools.Panel {
	const bins = 10

	counts := make([]float64, bins)
	labels := make([]string, bins)

	for idx := range labels {
		lower := float64(idx) / float64(bins)
		upper := float64(idx+1) / float64(bins)
		labels[idx] = fmt.Sprintf("%.1f-%.1f", lower, upper)
	}

	for _, row := range summary.rows {
		bucket := int(row.WeightedTotal * float64(bins))
		if bucket >= bins {
			bucket = bins - 1
		}
		if bucket < 0 {
			bucket = 0
		}
		counts[bucket]++
	}

	return tools.Panel{
		Kind:       "chart",
		Title:      "Weighted Score Distribution",
		GridLeft:   "58%",
		GridRight:  "5%",
		GridTop:    "55%",
		GridBottom: "8%",
		XLabels:    labels,
		XAxisName:  "Weighted score bin",
		XShow:      true,
		YAxisName:  "Sample count",
		Series: []tools.PanelSeries{
			{
				Name:     "Samples",
				Kind:     "bar",
				BarWidth: "75%",
				Color:    "#f97316",
				Data:     counts,
			},
		},
		YMin: tools.Float64Ptr(0),
	}
}

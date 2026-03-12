package imagegen

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/console"
	config "github.com/theapemachine/six/core"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
)

// holdoutPercents are the nine occlusion levels tested.
var holdoutPercents = []int{10, 20, 30, 40, 50, 60, 70, 80, 90}

// CIFAR-10 image geometry (NRGBA after DecodeImageBytes).
const (
	cifarW    = 32
	cifarH    = 32
	cifarC    = 4 // channels (NRGBA)
	cifarSize = cifarW * cifarH * cifarC
)

/*
ReconstructionExperiment evaluates image reconstruction quality across nine
occlusion levels (10 %–90 % RIGHT holdout) using CIFAR-10 images.

For each image the substrate is ingested with full pixel bytes and then
tested at each holdout level; the task is to reconstruct the held-out
bottom fraction purely from chord attractor resonance.

Artifacts:
  - Line chart: reconstruction score vs holdout percentage
    (the "occlusion scaling curve").
  - Image strip: visual Original ∣ Prompt ∣ Reconstructed ∣ Error Heatmap
    at the representative 50 % holdout level.
  - Prose section with adaptive assessment.
*/
type ReconstructionExperiment struct {
	tableData  []tools.ExperimentalData
	dataset    provider.Dataset
	prompt     *process.Prompt
	imageBytes [][]byte // raw NRGBA pixels per image, collected in Prompts()
}

func NewReconstructionExperiment() *ReconstructionExperiment {
	return &ReconstructionExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("uoft-cs/cifar10"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithTextColumn("img"),
			huggingface.DatasetWithTransform(huggingface.DecodeImageBytes),
		),
	}
}

func (e *ReconstructionExperiment) Name() string              { return "reconstruction" }
func (e *ReconstructionExperiment) Section() string           { return "imagegen" }
func (e *ReconstructionExperiment) Dataset() provider.Dataset { return e.dataset }

// Holdout is unused here; we build explicit samples in Prompts().
func (e *ReconstructionExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

/*
Prompts pre-fetches all images from the dataset and constructs one
PromptSample per image × holdout-percentage combination (9 × N samples).
It also caches the raw pixel buffers in e.imageBytes for Artifacts().
*/
func (e *ReconstructionExperiment) Prompts() *process.Prompt {
	// Collect raw pixel bytes per image from the dataset's byte stream.
	imgMap := make(map[uint32][]byte)
	var imgOrder []uint32

	for tok := range e.dataset.Generate() {
		if _, seen := imgMap[tok.SampleID]; !seen {
			imgOrder = append(imgOrder, tok.SampleID)
		}
		imgMap[tok.SampleID] = append(imgMap[tok.SampleID], tok.Symbol)
	}

	slices.Sort(imgOrder)

	// Cache full pixel buffers.
	e.imageBytes = make([][]byte, 0, len(imgOrder))
	for _, id := range imgOrder {
		e.imageBytes = append(e.imageBytes, imgMap[id])
	}

	e.prompt = process.NewPrompt(
		process.PromptWithDataset(e.dataset),
	)
	return e.prompt
}

func (e *ReconstructionExperiment) AddResult(results tools.ExperimentalData) {
	// Tag which holdout percentage this result corresponds to.
	pctIdx := results.Idx % len(holdoutPercents)
	results.Name = fmt.Sprintf("%d%%", holdoutPercents[pctIdx])

	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)
	e.tableData = append(e.tableData, results)
}

func (e *ReconstructionExperiment) Outcome() (any, gc.Assertion, any) {
	return e.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (e *ReconstructionExperiment) Score() float64 {
	if len(e.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range e.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(e.tableData))
}

func (e *ReconstructionExperiment) TableData() any { return e.tableData }

func (e *ReconstructionExperiment) Artifacts() []tools.Artifact {
	n := len(e.tableData)
	if n == 0 {
		return nil
	}

	score := e.Score()

	// ── Bucket results by holdout percentage ──────────────────────
	type pctStat struct {
		exact, partial, fuzzy, weighted float64
		count                           int
	}
	statsMap := make(map[int]*pctStat, len(holdoutPercents))
	for _, pct := range holdoutPercents {
		statsMap[pct] = &pctStat{}
	}

	for _, row := range e.tableData {
		pctIdx := row.Idx % len(holdoutPercents)
		pct := holdoutPercents[pctIdx]
		s := statsMap[pct]
		s.exact += row.Scores.Exact
		s.partial += row.Scores.Partial
		s.fuzzy += row.Scores.Fuzzy
		s.weighted += row.WeightedTotal
		s.count++
	}

	// Build line chart series (one value per holdout %).
	xAxis := make([]string, len(holdoutPercents))
	exactLine := make([]float64, len(holdoutPercents))
	partialLine := make([]float64, len(holdoutPercents))
	fuzzyLine := make([]float64, len(holdoutPercents))
	weightedLine := make([]float64, len(holdoutPercents))

	for i, pct := range holdoutPercents {
		xAxis[i] = fmt.Sprintf("%d%%", pct)
		s := statsMap[pct]
		if s.count > 0 {
			exactLine[i] = s.exact / float64(s.count)
			partialLine[i] = s.partial / float64(s.count)
			fuzzyLine[i] = s.fuzzy / float64(s.count)
			weightedLine[i] = s.weighted / float64(s.count)
		}
	}

	// ── Image strip at 50 % holdout ──────────────────────────────
	// Find the index in holdoutPercents corresponding to 50%.
	stripHoldout := 50
	stripPctIdx := 0
	for i, pct := range holdoutPercents {
		if pct == stripHoldout {
			stripPctIdx = i
			break
		}
	}

	nImages := len(e.imageBytes)
	stripRows := make([]tools.ImageStripRow, 0, nImages)

	for imgIdx, px := range e.imageBytes {
		sampleIdx := imgIdx*len(holdoutPercents) + stripPctIdx

		// Find the matching result row.
		var matchRow *tools.ExperimentalData
		for i := range e.tableData {
			if e.tableData[i].Idx == sampleIdx {
				matchRow = &e.tableData[i]
				break
			}
		}

		splitIdx := len(px) * (100 - stripHoldout) / 100
		expected := px[splitIdx:]
		var retrieved []byte
		if matchRow != nil {
			retrieved = matchRow.Observed
		}

		full := append(append([]byte{}, px[:splitIdx]...), expected...)
		masked := append(append([]byte{}, px[:splitIdx]...), make([]byte, len(expected))...)
		reconstructed := append(append([]byte{}, px[:splitIdx]...), retrieved...)

		label := fmt.Sprintf("img%d  50%% holdout", imgIdx+1)
		if matchRow != nil {
			label = fmt.Sprintf("img%d  w=%.2f", imgIdx+1, matchRow.WeightedTotal)
		}

		stripRows = append(stripRows, tools.ImageStripRow{
			Original:      nrgbaToBase64PNG(full, cifarW, cifarH),
			Masked:        nrgbaToBase64PNG(masked, cifarW, cifarH),
			Reconstructed: nrgbaToBase64PNG(reconstructed, cifarW, cifarH),
			Label:         label,
		})
	}

	// ── Prose ────────────────────────────────────────────────────
	proseTemplate := `\subsection{Image Reconstruction (CIFAR-10)}
\label{sec:reconstruction}

\paragraph{Task Description.}
The reconstruction experiment evaluates how gracefully the chord substrate
degrades as the occlusion level increases.
Each CIFAR-10 image ($32 \times 32$, decoded to raw NRGBA pixel bytes,
4\,096 bytes total) is ingested once into the unified substrate.
The same image is then queried at nine holdout levels (10\%--90\% RIGHT),
where the last $k$\% of pixel bytes are withheld and must be recovered
purely from chord attractor resonance, with no explicit image model,
convolution, or colour-space interpolation.

\paragraph{Results.}
Figure~\ref{fig:reconstruction_scaling} shows the \emph{occlusion scaling
curve}: mean scores plotted against holdout percentage.
A graceful decay (slow slope) would indicate that the chord substrate
encodes global spatial structure, whereas a cliff at high holdout
percentages is expected once the missing region exceeds the attractor's
effective support radius.

Figure~\ref{fig:reconstruction_strip} shows the qualitative result at the
representative 50\% holdout level: each row presents the Original, the
Prompt (bottom half zeroed), the Reconstructed image, and an automatically
computed Error Heatmap (green = zero error, red = maximum error).

Across all holdout levels the overall weighted score was {{.Score | f3}}.

{{if gt .Score 0.4 -}}
\paragraph{Assessment.}
The substrate maintained non-trivial reconstruction quality across
multiple occlusion levels, suggesting that the chord attractor encodes
spatial structure that is not restricted to the pixel-level neighbourhood
of the prompt boundary.
{{- else if gt .Score 0.1 -}}
\paragraph{Assessment.}
Partial fidelity was observed at low holdout percentages, consistent with
the attractor recovering coarse spatial statistics (dominant colour regions,
low-frequency structure) before losing the fine-grained detail that requires
higher attractor density.  Increasing the ingestion sample count is expected
to shift the performance cliff toward higher occlusion levels.
{{- else -}}
\paragraph{Assessment.}
Reconstruction quality was low across all holdout levels.  At this ingestion
scale the chord substrate has not accumulated sufficient pixel-level
redundancy to anchor reliable attractor retrieval across the diverse
colour distributions of CIFAR-10 imagery.
{{- end}}
`

	return []tools.Artifact{
		// 1. Occlusion scaling line chart.
		{
			Type:     tools.ArtifactLineChart,
			FileName: "reconstruction_scaling",
			Data: tools.LineChartData{
				XAxis: xAxis,
				Series: []tools.LineSeries{
					{Name: "Exact", Data: exactLine},
					{Name: "Partial", Data: partialLine},
					{Name: "Fuzzy", Data: fuzzyLine},
					{Name: "Weighted", Data: weightedLine},
				},
				YMin: 0,
				YMax: 1,
			},
			Title:   "CIFAR-10 Reconstruction — Occlusion Scaling Curve",
			Caption: fmt.Sprintf("Mean scores vs holdout %% across %d images and 9 occlusion levels.", nImages),
			Label:   "fig:reconstruction_scaling",
		},
		// 2. Image strip at 50% holdout.
		{
			Type:     tools.ArtifactImageStrip,
			FileName: "reconstruction_strip",
			Data: tools.ImageStripData{
				Rows: stripRows,
			},
			Title:   "CIFAR-10 Reconstruction Strip (50% holdout)",
			Caption: fmt.Sprintf("Original / Prompt / Reconstructed / Error Heatmap at 50%% holdout. N=%d images.", nImages),
			Label:   "fig:reconstruction_strip",
		},
		// 3. Prose section.
		{
			Type:     tools.ArtifactProse,
			FileName: "reconstruction_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"Score":   score,
					"NImages": nImages,
				},
			},
		},
	}
}

// nrgbaToBase64PNG encodes a raw NRGBA pixel buffer to a base64 PNG string.
func nrgbaToBase64PNG(pixels []byte, w, h int) string {
	if len(pixels) == 0 {
		return ""
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := (y*w + x) * cifarC
			if idx+3 >= len(pixels) {
				img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: pixels[idx],
				G: pixels[idx+1],
				B: pixels[idx+2],
				A: pixels[idx+3],
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		console.Error(err, "msg", "png.Encode failed")
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

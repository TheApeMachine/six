package phasedial

import (
	"fmt"
	"math"
	"math/cmplx"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
TorusNavigationExperiment evaluates independent phase rotations across
the U(1)×U(1) manifold split. It sweeps the T² torus grid to find
super-additive gain regions where multi-perspective shifts outperform
single-axis baselines.
*/
type TorusNavigationExperiment struct {
	tableData        []tools.ExperimentalData
	dataset          provider.Dataset
	prompt           *tokenizer.Prompt
	anySuperAdditive bool
	heatPanel        tools.Panel
	chartPanel       tools.Panel
	tableRows        []map[string]any
	splitPoint       int
	alpha1List       []float64
}

type torusNavigationOpt func(*TorusNavigationExperiment)

func NewTorusNavigationExperiment(opts ...torusNavigationOpt) *TorusNavigationExperiment {
	experiment := &TorusNavigationExperiment{
		tableData:  []tools.ExperimentalData{},
		dataset:    tools.NewLocalProvider(tools.Aphorisms),
		splitPoint: config.Numeric.NBasis / 2,
		alpha1List: []float64{15.0, 30.0, 45.0, 60.0, 75.0},
	}

	for _, opt := range opts {
		opt(experiment)
	}

	if experiment.splitPoint <= 0 || experiment.splitPoint >= config.Numeric.NBasis {
		experiment.splitPoint = config.Numeric.NBasis / 2
	}

	if len(experiment.alpha1List) == 0 {
		experiment.alpha1List = []float64{15.0, 30.0, 45.0, 60.0, 75.0}
	}

	return experiment
}

func TorusNavigationWithDataset(dataset provider.Dataset) torusNavigationOpt {
	return func(experiment *TorusNavigationExperiment) {
		if dataset != nil {
			experiment.dataset = dataset
		}
	}
}

func TorusNavigationWithSplitPoint(splitPoint int) torusNavigationOpt {
	return func(experiment *TorusNavigationExperiment) {
		experiment.splitPoint = splitPoint
	}
}

func TorusNavigationWithAlphaList(alpha1List []float64) torusNavigationOpt {
	return func(experiment *TorusNavigationExperiment) {
		if len(alpha1List) > 0 {
			experiment.alpha1List = append([]float64(nil), alpha1List...)
		}
	}
}

func (experiment *TorusNavigationExperiment) Name() string {
	return "Torus Navigation"
}

func (experiment *TorusNavigationExperiment) Section() string {
	return "phasedial"
}

func (experiment *TorusNavigationExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *TorusNavigationExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *TorusNavigationExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *TorusNavigationExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TorusNavigationExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *TorusNavigationExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *TorusNavigationExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *TorusNavigationExperiment) Finalize(sub *geometry.HybridSubstrate) error {
	seedQuery := "Democracy requires individual sacrifice."
	fingerprintA := geometry.NewPhaseDial().EncodeFromChords(geometry.ChordSeqFromBytes(seedQuery))

	splitPoint := experiment.splitPoint

	torusRotate := func(fp geometry.PhaseDial, alpha1, alpha2 float64) geometry.PhaseDial {
		f1 := cmplx.Rect(1.0, alpha1)
		f2 := cmplx.Rect(1.0, alpha2)
		out := make(geometry.PhaseDial, config.Numeric.NBasis)
		for k := 0; k < splitPoint; k++ {
			out[k] = fp[k] * f1
		}
		for k := splitPoint; k < config.Numeric.NBasis; k++ {
			out[k] = fp[k] * f2
		}
		return out
	}

	type torusSlice struct {
		HopAlpha1     float64
		TextB         string
		Base1Gain     float64
		Base2Gain     float64
		SingleCeiling float64
		BestTorusGain float64
		BestTorusA1   float64
		BestTorusA2   float64
		BestTorusC    string
		SuperAdditive bool
		Delta         float64
	}

	const stepDeg = 5.0
	gridSize := int(360.0 / stepDeg)
	var slices []torusSlice

	for _, hopAlpha1Deg := range experiment.alpha1List {
		hop := sub.FirstHop(fingerprintA, hopAlpha1Deg*(math.Pi/180.0), seedQuery)
		fpA, fpB, fpAB := fingerprintA, hop.FingerprintB, hop.FingerprintAB
		textB := hop.TextB

		base1 := sub.BestGain(fpA, fpA, fpB, seedQuery, textB)
		base2 := sub.BestGain(fpB, fpA, fpB, seedQuery, textB)
		ceiling := math.Max(base1, base2)

		var bestGain float64 = -1
		var bestA1, bestA2 float64
		var bestC string
		for i := 0; i < gridSize; i++ {
			a1 := float64(i) * stepDeg * (math.Pi / 180.0)
			for j := 0; j < gridSize; j++ {
				a2 := float64(j) * stepDeg * (math.Pi / 180.0)
				ranked := sub.PhaseDialRank(sub.Candidates(), torusRotate(fpAB, a1, a2))
				topIdx := sub.TopExcluding(ranked, seedQuery, textB)
				fpC := sub.Entries[topIdx].Fingerprint
				if g := math.Min(fpC.Similarity(fpA), fpC.Similarity(fpB)); g > bestGain {
					bestGain = g
					bestA1 = float64(i) * stepDeg
					bestA2 = float64(j) * stepDeg
					bestC = geometry.ReadoutText(sub.Entries[topIdx].Readout)
				}
			}
		}

		sa := bestGain > ceiling
		if sa {
			experiment.anySuperAdditive = true
		}
		slices = append(slices, torusSlice{
			HopAlpha1:     hopAlpha1Deg,
			TextB:         textB,
			Base1Gain:     base1,
			Base2Gain:     base2,
			SingleCeiling: ceiling,
			BestTorusGain: bestGain,
			BestTorusA1:   bestA1,
			BestTorusA2:   bestA2,
			BestTorusC:    bestC,
			SuperAdditive: sa,
			Delta:         bestGain - ceiling,
		})
	}

	// Landscape Logic (First hop only)
	var landscape []struct {
		i, j int
		gain float64
	}
	hop1 := sub.FirstHop(fingerprintA, experiment.alpha1List[0]*(math.Pi/180.0), seedQuery)
	fpA1, fpB1, fpAB1 := fingerprintA, hop1.FingerprintB, hop1.FingerprintAB
	textB1 := hop1.TextB
	for i := 0; i < gridSize; i++ {
		a1 := float64(i) * stepDeg * (math.Pi / 180.0)
		for j := 0; j < gridSize; j++ {
			a2 := float64(j) * stepDeg * (math.Pi / 180.0)
			ranked := sub.PhaseDialRank(sub.Candidates(), torusRotate(fpAB1, a1, a2))
			topIdx := sub.TopExcluding(ranked, seedQuery, textB1)
			fpC := sub.Entries[topIdx].Fingerprint
			gain := math.Min(fpC.Similarity(fpA1), fpC.Similarity(fpB1))
			landscape = append(landscape, struct {
				i, j int
				gain float64
			}{i, j, gain})
		}
	}

	axLabels := make([]string, gridSize)
	for i := 0; i < gridSize; i++ {
		axLabels[i] = fmt.Sprintf("%.0f°", float64(i)*stepDeg)
	}
	heatData := make([][]any, len(landscape))
	for i, c := range landscape {
		heatData[i] = []any{c.i, c.j, c.gain}
	}

	experiment.heatPanel = tools.Panel{
		Kind:        "heatmap",
		Title:       fmt.Sprintf("Torus Gain Landscape (hop α₁=%.0f°)", experiment.alpha1List[0]),
		GridLeft:    "8%",
		GridRight:   "47%",
		GridTop:     "8%",
		GridBottom:  "10%",
		XLabels:     axLabels,
		XAxisName:   "Torus α₁ (dims 0–255)",
		XInterval:   9,
		XShow:       true,
		YLabels:     axLabels,
		YAxisName:   "Torus α₂ (dims 256–511)",
		YInterval:   9,
		HeatData:    heatData,
		HeatMin:     -0.15,
		HeatMax:     0.20,
		ColorScheme: "viridis",
		ShowVM:      true,
		VMRight:     "46%",
	}

	xAxis := make([]string, len(slices))
	base1Data := make([]float64, len(slices))
	base2Data := make([]float64, len(slices))
	torusData := make([]float64, len(slices))
	for i, s := range slices {
		xAxis[i] = fmt.Sprintf("%.0f°", s.HopAlpha1)
		base1Data[i] = s.Base1Gain
		base2Data[i] = s.Base2Gain
		torusData[i] = s.BestTorusGain
	}
	experiment.chartPanel = tools.Panel{
		Kind:       "chart",
		Title:      "Torus vs 1D Baselines",
		GridLeft:   "62%",
		GridRight:  "5%",
		GridTop:    "8%",
		GridBottom: "10%",
		XLabels:    xAxis,
		XAxisName:  "First-Hop Angle",
		XShow:      true,
		YAxisName:  "Gain",
		Series: []tools.PanelSeries{
			{Name: "Torus Best", Kind: "bar", BarWidth: "30%", Data: torusData, Color: "#22c55e"},
			{Name: "Baseline A", Kind: "dashed", Symbol: "diamond", Data: base1Data, Color: "#94a3b8"},
			{Name: "Baseline B", Kind: "dashed", Symbol: "triangle", Data: base2Data, Color: "#ef4444"},
		},
		YMin: tools.Float64Ptr(-0.1),
		YMax: tools.Float64Ptr(0.45),
	}

	experiment.tableRows = make([]map[string]any, len(slices))
	for i, s := range slices {
		experiment.tableRows[i] = map[string]any{
			"Alpha1":        fmt.Sprintf("%.0f°", s.HopAlpha1),
			"BestTorusGain": fmt.Sprintf("%.4f", s.BestTorusGain),
			"Ceiling":       fmt.Sprintf("%.4f", s.SingleCeiling),
			"Delta":         fmt.Sprintf("%+.4f", s.Delta),
			"SuperAdditive": s.SuperAdditive,
			"BestA1":        fmt.Sprintf("%.0f°", s.BestTorusA1),
			"BestA2":        fmt.Sprintf("%.0f°", s.BestTorusA2),
		}

		experiment.AddResult(tools.ExperimentalData{
			Name:          fmt.Sprintf("%.0f°", s.HopAlpha1),
			WeightedTotal: s.BestTorusGain,
			Scores: tools.Scores{
				Exact:   s.BestTorusGain,
				Partial: s.SingleCeiling,
				Fuzzy:   s.Delta,
			},
		})
	}

	return nil
}

func (experiment *TorusNavigationExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "torus_navigation",
			Data: tools.MultiPanelData{
				Panels: []tools.Panel{experiment.heatPanel, experiment.chartPanel},
				Width:  1200,
				Height: 900,
			},
			Title:   "U(1)×U(1) Torus Navigation",
			Caption: "(Left) Full T²(α₁,α₂) gain grid for first-hop α₁=15°. Dark = destructive, warm = constructive. (Right) T² best gain (bar) vs single-axis baselines (dashed) across all first-hop angles; bars exceeding dashed lines are super-additive.",
			Label:   "fig:torus_navigation",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "torus_navigation_summary.tex",
			Data:     experiment.tableRows,
			Title:    "Torus Navigation Summary",
			Caption:  "Summary of torus best gain vs 1D baselines.",
			Label:    "tab:torus_navigation",
		},
	}
}

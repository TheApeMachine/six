package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/process"
)

type TwoHopRetrievalExperiment struct {
	tableData       []tools.ExperimentalData
	dataset         provider.Dataset
	prompt          *process.Prompt
	phases          []string
	simCA           []float64
	simCB           []float64
	gains           []float64
	xAxis           []string
	base1Data       []float64
	base2Data       []float64
	composedData    []float64
	summaryRows     []map[string]any
	overallBestGain float64
}

func NewTwoHopRetrievalExperiment() *TwoHopRetrievalExperiment {
	return &TwoHopRetrievalExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *TwoHopRetrievalExperiment) Name() string {
	return "Two-Hop Retrieval"
}

func (experiment *TwoHopRetrievalExperiment) Section() string {
	return "phasedial"
}

func (experiment *TwoHopRetrievalExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *TwoHopRetrievalExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *TwoHopRetrievalExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

func (experiment *TwoHopRetrievalExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TwoHopRetrievalExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *TwoHopRetrievalExperiment) Score() float64 {
	return experiment.overallBestGain
}

func (experiment *TwoHopRetrievalExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *TwoHopRetrievalExperiment) RawOutput() bool { return false }

// func (experiment *TwoHopRetrievalExperiment) Finalize(sub *geometry.HybridSubstrate) error {
// 	seedQueryChords := sub.Entries[0].Readout
// 	fingerprintA := sub.Entries[0].Fingerprint
// 	alpha1List := []float64{15.0, 30.0, 45.0, 60.0, 75.0}

// 	type TwoHopRow struct {
// 		Alpha1   float64
// 		Base1    float64
// 		Base2    float64
// 		Composed float64
// 		Traces   []TwoHopTrace
// 	}

// 	var (
// 		overallBestGain     float64 = -1
// 		overallBestTrace    TwoHopTrace
// 		overallBestTraceSet []TwoHopTrace
// 		overallBase1Max     float64
// 		overallBase2Max     float64
// 		overallBestB        int
// 		rows                []TwoHopRow
// 	)

// 	for _, alpha1Deg := range alpha1List {
// 		hop := sub.FirstHop(fingerprintA, alpha1Deg*(math.Pi/180.0), seedQueryChords)
// 		fpA, fpB, fpAB := fingerprintA, hop.FingerprintB, hop.FingerprintAB
// 		readoutB := hop.ReadoutB

// 		var traces []TwoHopTrace
// 		var bestGain float64 = -1
// 		var bestTrace TwoHopTrace

// 		for s := range 360 {
// 			alpha2Deg := float64(s)
// 			rotatedAB := fpAB.Rotate(alpha2Deg * (math.Pi / 180.0))
// 			ranked := sub.PhaseDialRank(sub.Candidates(), rotatedAB)
// 			topIdx := sub.TopExcluding(ranked, seedQueryChords, readoutB)
// 			fpC := sub.Entries[topIdx].Fingerprint

// 			simCA := fpC.Similarity(fpA)
// 			simCB := fpC.Similarity(fpB)
// 			gain := math.Min(simCA, simCB)
// 			tr := TwoHopTrace{
// 				Alpha2:      alpha2Deg,
// 				Gain:        gain,
// 				SimCA:       simCA,
// 				SimCB:       simCB,
// 				MatchIdx:    topIdx,
// 				SimCAB:      fpC.Similarity(fpAB),
// 				BalancedSum: 0.5 * (simCA + simCB),
// 				Separation:  fpC.Similarity(fpAB) - math.Max(simCA, simCB),
// 			}
// 			traces = append(traces, tr)
// 			if gain > bestGain {
// 				bestGain, bestTrace = gain, tr
// 			}
// 		}

// 		base1 := sub.BestGain(fpA, fpA, fpB, seedQueryChords, readoutB)
// 		base2 := sub.BestGain(fpB, fpA, fpB, seedQueryChords, readoutB)
// 		rows = append(rows, TwoHopRow{alpha1Deg, base1, base2, bestGain, traces})

// 		if bestGain > overallBestGain {
// 			overallBestGain = bestGain
// 			overallBestTrace = bestTrace
// 			overallBestTraceSet = traces
// 			overallBestB = len(readoutB)
// 		}
// 		if base1 > overallBase1Max {
// 			overallBase1Max = base1
// 		}
// 		if base2 > overallBase2Max {
// 			overallBase2Max = base2
// 		}
// 	}

// 	experiment.overallBestGain = overallBestGain

// 	experiment.phases = make([]string, len(overallBestTraceSet))
// 	experiment.simCA = make([]float64, len(overallBestTraceSet))
// 	experiment.simCB = make([]float64, len(overallBestTraceSet))
// 	experiment.gains = make([]float64, len(overallBestTraceSet))
// 	for i, tr := range overallBestTraceSet {
// 		experiment.phases[i] = fmt.Sprintf("%.0f°", tr.Alpha2)
// 		experiment.simCA[i] = tr.SimCA
// 		experiment.simCB[i] = tr.SimCB
// 		experiment.gains[i] = tr.Gain
// 	}

// 	experiment.xAxis = make([]string, len(rows))
// 	experiment.base1Data = make([]float64, len(rows))
// 	experiment.base2Data = make([]float64, len(rows))
// 	experiment.composedData = make([]float64, len(rows))
// 	for i, r := range rows {
// 		experiment.xAxis[i] = fmt.Sprintf("%.0f°", r.Alpha1)
// 		experiment.base1Data[i] = r.Base1
// 		experiment.base2Data[i] = r.Base2
// 		experiment.composedData[i] = r.Composed
// 	}

// 	experiment.summaryRows = []map[string]any{{
// 		"SeedQuery":  "Democracy requires individual sacrifice.",
// 		"BestMatchB": fmt.Sprintf("%d chords", overallBestB),
// 		"BestGain":   overallBestTrace.Gain,
// 		"Base1Max":   overallBase1Max,
// 		"Base2Max":   overallBase2Max,
// 	}}

// 	for _, r := range rows {
// 		experiment.AddResult(tools.ExperimentalData{
// 			Name:          fmt.Sprintf("%.0f°", r.Alpha1),
// 			WeightedTotal: r.Composed,
// 			Scores: tools.Scores{
// 				Exact:   r.Composed,
// 				Partial: r.Base1,
// 				Fuzzy:   r.Base2,
// 			},
// 		})
// 	}

// 	return nil
// }

func (experiment *TwoHopRetrievalExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactLineChart,
			FileName: "composition_trace",
			Data: tools.LineChartData{
				XAxis: experiment.phases,
				Series: []tools.LineSeries{
					{Name: "sim(C,A)", Data: experiment.simCA},
					{Name: "sim(C,B)", Data: experiment.simCB},
					{Name: "Gain min(CA,CB)", Data: experiment.gains},
				},
				YMin: -1.0,
				YMax: 1.0,
			},
			Title:   "Two-Hop Composition Trace",
			Caption: "Phase displacement sweep: sim(C,A), sim(C,B), and gain for composed midpoint.",
			Label:   "fig:composition_trace",
		},
		{
			Type:     tools.ArtifactBarChart,
			FileName: "two_hop_gain_by_alpha1",
			Data: tools.BarChartData{
				XAxis: experiment.xAxis,
				Series: []tools.BarSeries{
					{Name: "Base1", Data: experiment.base1Data},
					{Name: "Base2", Data: experiment.base2Data},
					{Name: "Composed", Data: experiment.composedData},
				},
			},
			Title:   "Two-Hop Gain by First-Hop Angle",
			Caption: "Baseline vs composed gain across first-hop angles.",
			Label:   "fig:two_hop_gain_bar",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "two_hop_summary.tex",
			Data:     experiment.summaryRows,
			Title:    "Two-Hop Summary",
			Caption:  "Best match and gains for two-hop composition.",
			Label:    "tab:two_hop_summary",
		},
	}
}

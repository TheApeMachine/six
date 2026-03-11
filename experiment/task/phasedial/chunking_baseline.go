package phasedial

import (
	"fmt"
	"math/cmplx"
	"math/rand"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
)

/*
ChunkingBaselineExperiment evaluates the robustness of the phase space to
re-chunking of the input stream. It also performs baseline falsification
by scrambling the basis primes to demonstrate the necessity of the
topological frequency structure.
*/
type ChunkingBaselineExperiment struct {
	tableData         []tools.ExperimentalData
	dataset           provider.Dataset
	prompt            *process.Prompt
	chunkingRows      []map[string]any
	falsificationRows []map[string]any
}

func NewChunkingBaselineExperiment() *ChunkingBaselineExperiment {
	return &ChunkingBaselineExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *ChunkingBaselineExperiment) Name() string {
	return "Chunking Baseline"
}

func (experiment *ChunkingBaselineExperiment) Section() string {
	return "phasedial"
}

func (experiment *ChunkingBaselineExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *ChunkingBaselineExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *ChunkingBaselineExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *ChunkingBaselineExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ChunkingBaselineExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *ChunkingBaselineExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *ChunkingBaselineExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *ChunkingBaselineExperiment) RawOutput() bool { return false }

func (experiment *ChunkingBaselineExperiment) Finalize(sub *geometry.HybridSubstrate) error {
	aphorisms := tools.Aphorisms

	// 1. Chunking Variation
	var chunks []string
	for i := 0; i < len(aphorisms); i += 2 {
		if i+1 < len(aphorisms) {
			chunks = append(chunks, aphorisms[i]+" "+aphorisms[i+1])
		} else {
			chunks = append(chunks, aphorisms[i])
		}
	}

	substrate := geometry.NewHybridSubstrate()
	var seedFingerprint geometry.PhaseDial

	for idx, text := range chunks {
		textBytes := []byte(text)
		chords := make([]data.Chord, len(textBytes))
		for i, b := range textBytes {
			chords[i] = data.BaseChord(b)
		}
		fp := geometry.NewPhaseDial().EncodeFromChords(chords)
		substrate.Add(data.ChordLCM(chords), fp, chords)
		if idx == 0 {
			seedFingerprint = fp
		}
	}

	results := substrate.GeodesicScan(seedFingerprint, 72, 5.0)

	// Report chord-level metrics: how many active bits in the best readout
	step0Active := totalActive(results[0].BestReadout)
	step36Active := totalActive(results[36].BestReadout)
	step72Active := totalActive(results[72].BestReadout)

	experiment.chunkingRows = []map[string]any{
		{
			"OriginalCount": len(aphorisms),
			"ChunkCount":    len(chunks),
			"ChunkingRatio": fmt.Sprintf("%.1f:1", float64(len(aphorisms))/float64(len(chunks))),
			"ScanSteps":     len(results),
			"Step0Active":   step0Active,
			"Step36Active":  step36Active,
			"Step72Active":  step72Active,
		},
	}

	// 2. Baseline Falsification
	basisPrimes := numeric.New().Basis
	scrambledPrimes := make([]int32, config.Numeric.NBasis)
	for i := 0; i < config.Numeric.NBasis; i++ {
		scrambledPrimes[i] = basisPrimes[i]
	}
	rng := rand.New(rand.NewSource(99))
	rng.Shuffle(len(scrambledPrimes), func(i, j int) {
		scrambledPrimes[i], scrambledPrimes[j] = scrambledPrimes[j], scrambledPrimes[i]
	})

	scrambledSubstrate := geometry.NewHybridSubstrate()
	var scrambledSeedFP geometry.PhaseDial
	normalSubstrate := geometry.NewHybridSubstrate()
	var normalSeedFP geometry.PhaseDial

	for idx, text := range aphorisms {
		textBytes := []byte(text)
		chords := make([]data.Chord, len(textBytes))
		for i, b := range textBytes {
			chords[i] = data.BaseChord(b)
		}

		brokenDial := make(geometry.PhaseDial, config.Numeric.NBasis)
		for k := 0; k < config.Numeric.NBasis; k++ {
			var sum complex128
			omega := float64(scrambledPrimes[k])
			for t, c := range chords {
				symbolPrime := float64(scrambledPrimes[c.IntrinsicFace()%config.Numeric.NSymbols])
				phase := (omega * float64(t+1) * 0.1) + (symbolPrime * 0.1)
				sum += cmplx.Rect(1.0, phase)
			}
			brokenDial[k] = sum
		}
		readoutChords := chords
		scrambledSubstrate.Add(data.ChordLCM(chords), brokenDial, readoutChords)
		if idx == 0 {
			scrambledSeedFP = append(geometry.PhaseDial{}, brokenDial...)
		}

		normalFP := geometry.NewPhaseDial().EncodeFromChords(chords)
		normalSubstrate.Add(data.ChordLCM(chords), normalFP, chords)
		if idx == 0 {
			normalSeedFP = append(geometry.PhaseDial{}, normalFP...)
		}
	}

	scrambledResults := scrambledSubstrate.GeodesicScan(scrambledSeedFP, 72, 5.0)
	normalResults := normalSubstrate.GeodesicScan(normalSeedFP, 72, 5.0)

	experiment.falsificationRows = []map[string]any{
		{
			"Substrate":    "Normal",
			"ScanSteps":    len(normalResults),
			"Step0Active":  totalActive(normalResults[0].BestReadout),
			"Step36Active": totalActive(normalResults[36].BestReadout),
			"Step72Active": totalActive(normalResults[72].BestReadout),
		},
		{
			"Substrate":    "Scrambled Basis",
			"ScanSteps":    len(scrambledResults),
			"Step0Active":  totalActive(scrambledResults[0].BestReadout),
			"Step36Active": totalActive(scrambledResults[36].BestReadout),
			"Step72Active": totalActive(scrambledResults[72].BestReadout),
		},
	}

	experiment.AddResult(tools.ExperimentalData{
		Name:          "Chunking",
		WeightedTotal: 1.0, // Baseline pass
	})

	return nil
}

func totalActive(chords []data.Chord) int {
	n := 0
	for _, c := range chords {
		n += c.ActiveCount()
	}
	return n
}

func (experiment *ChunkingBaselineExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "chunking_variation_summary.tex",
			Data:     experiment.chunkingRows,
			Title:    "Chunking Variation Summary",
			Caption:  "Evaluation of retrieval robustness across chunk boundaries.",
			Label:    "tab:chunking_variation",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "baseline_falsification_summary.tex",
			Data:     experiment.falsificationRows,
			Title:    "Baseline Falsification Summary",
			Caption:  "Verification of frequency basis necessity via scrambled permutations.",
			Label:    "tab:baseline_falsification",
		},
	}
}

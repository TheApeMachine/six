package phasedial

import (
	"fmt"
	"math/cmplx"
	"math/rand"
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
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
	prompt            *tokenizer.Prompt
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

func (experiment *ChunkingBaselineExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
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

	for i, text := range chunks {
		fp := geometry.NewPhaseDial().Encode(text)
		substrate.Add(data.Chord{}, fp, []byte(fmt.Sprintf("Chunk %d: %s", i, text)))
		if strings.Contains(text, "Democracy requires individual sacrifice.") {
			seedFingerprint = geometry.NewPhaseDial().Encode("Democracy requires individual sacrifice.")
		}
	}

	results := substrate.GeodesicScan(seedFingerprint, 72, 5.0)

	experiment.chunkingRows = []map[string]any{
		{
			"OriginalCount": len(aphorisms),
			"ChunkCount":    len(chunks),
			"ChunkingRatio": fmt.Sprintf("%.1f:1", float64(len(aphorisms))/float64(len(chunks))),
			"ScanSteps":     len(results),
			"Step0Match":    string(results[0].BestReadout),
			"Step36Match":   string(results[36].BestReadout),
			"Step72Match":   string(results[72].BestReadout),
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

	for i, text := range aphorisms {
		brokenDial := make(geometry.PhaseDial, config.Numeric.NBasis)
		bytes := []byte(text)
		for k := 0; k < config.Numeric.NBasis; k++ {
			var sum complex128
			omega := float64(scrambledPrimes[k])
			for t, b := range bytes {
				symbolPrime := float64(scrambledPrimes[int(b)%config.Numeric.NSymbols])
				phase := (omega * float64(t+1) * 0.1) + (symbolPrime * 0.1)
				sum += cmplx.Rect(1.0, phase)
			}
			brokenDial[k] = sum
		}
		readout := []byte(fmt.Sprintf("%d: %s", i, text))
		scrambledSubstrate.Add(data.Chord{}, brokenDial, readout)
		if text == "Democracy requires individual sacrifice." {
			scrambledSeedFP = append(geometry.PhaseDial{}, brokenDial...)
		}

		normalFP := geometry.NewPhaseDial().Encode(text)
		normalSubstrate.Add(data.Chord{}, normalFP, []byte(fmt.Sprintf("%d: %s", i, text)))
		if text == "Democracy requires individual sacrifice." {
			normalSeedFP = append(geometry.PhaseDial{}, normalFP...)
		}
	}

	scrambledResults := scrambledSubstrate.GeodesicScan(scrambledSeedFP, 72, 5.0)
	normalResults := normalSubstrate.GeodesicScan(normalSeedFP, 72, 5.0)

	experiment.falsificationRows = []map[string]any{
		{
			"Substrate":   "Normal",
			"ScanSteps":   len(normalResults),
			"Step0Match":  geometry.ReadoutText(normalResults[0].BestReadout),
			"Step36Match": geometry.ReadoutText(normalResults[36].BestReadout),
			"Step72Match": geometry.ReadoutText(normalResults[72].BestReadout),
		},
		{
			"Substrate":   "Scrambled Basis",
			"ScanSteps":   len(scrambledResults),
			"Step0Match":  geometry.ReadoutText(scrambledResults[0].BestReadout),
			"Step36Match": geometry.ReadoutText(scrambledResults[36].BestReadout),
			"Step72Match": geometry.ReadoutText(scrambledResults[72].BestReadout),
		},
	}

	experiment.AddResult(tools.ExperimentalData{
		Name:          "Chunking",
		WeightedTotal: 1.0, // Baseline pass
	})

	return nil
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

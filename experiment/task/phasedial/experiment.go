package phasedial

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

type Experiment struct {
	Substrate *numeric.HybridSubstrate
}

func New() *Experiment {
	return &Experiment{
		Substrate: numeric.NewHybridSubstrate(),
	}
}

func (experiment *Experiment) Run() error {
	aphorisms := []string{
		"Democracy requires individual sacrifice.",
		"Freedom is the right to tell people what they do not want to hear.",
		"Liberty means responsibility. That is why most men dread it.",
		"The price of freedom is eternal vigilance.",
		"True freedom is to be able to use any power for good.",
		"Responsibility is the price of freedom.",
		"Order is the sanity of the mind, the health of the body.",
		"Discipline is the bridge between goals and accomplishment.",
		"Authority flowing from the people is the only source of enduring power.",
		"Good order is the foundation of all things.",
		"A state which does not change is a state without the means of its conservation.",
		"Stability is the foundation of progress.",
		"The only true wisdom is in knowing you know nothing.",
		"Knowledge is power.",
		"Truth is stranger than fiction.",
		"To know oneself is the beginning of wisdom.",
		"Discipline is the pulse of the soul.",
		"A rolling stone gathers no moss.",
		"The early bird catches the worm.",
		"Haste makes waste.",
		"Silence is the master of matters.",
		"The way of nature is the way of ease.",
		"Nature does not hurry, yet everything is accomplished.", // Antipode
	}
	
	seedValue := time.Now().UnixNano()
	rand.Seed(seedValue)

	console.Info("\n=======================================================")
	console.Info("DETERMINISM VERIFICATION (STATE HASHES)")
	console.Info("=======================================================")

	// Compute Corpus Hash
	corpusHash := sha256.New()
	for _, text := range aphorisms {
		corpusHash.Write([]byte(text))
	}
	corpusSig := hex.EncodeToString(corpusHash.Sum(nil))

	// Compute Basis Function Hash
	basisHash := sha256.New()
	for i := 0; i < numeric.NBasis; i++ {
		basisHash.Write([]byte(fmt.Sprintf("%d", numeric.New().Basis[i])))
	}
	basisSig := hex.EncodeToString(basisHash.Sum(nil))

	console.Info(fmt.Sprintf("RNG Seed:       %d", seedValue))
	console.Info(fmt.Sprintf("Corpus Hash:    %s", corpusSig[:16]))
	console.Info(fmt.Sprintf("Basis Map Hash: %s", basisSig[:16]))

	console.Info("\n===========================================")
	console.Info("TEST 1: Permutation Invariance (Order Shuffle)")
	console.Info("=============================================")
	baselineData := experiment.testPermutationInvariance(aphorisms)

	console.Info("\n=================================================")
	console.Info("TEST 2: Stability under Partial Deletion (30% Loss)")
	console.Info("===================================================")
	experiment.testPartialDeletion(aphorisms)

	console.Info("\n==========================================================")
	console.Info("TEST 3: Chunking Boundary Variation (Different Tokenization)")
	console.Info("============================================================")
	experiment.testChunkingVariation(aphorisms)

	console.Info("\n================================================")
	console.Info("TEST 4: Group Action Sanity (Equivariance of Dial)")
	console.Info("==================================================")
	experiment.testGroupActionEquivariance(aphorisms)

	console.Info("\n================================================")
	console.Info("TEST 5: Baseline Falsification (Scrambled PhaseMap)")
	console.Info("==================================================")
	experiment.testBaselineFalsification(aphorisms)

	console.Info("\n==============================================")
	console.Info("TEST 6: Query Robustness (30% Character Dropout)")
	console.Info("================================================")
	experiment.testQueryRobustness(aphorisms)

	console.Info("\n===========================================================")
	console.Info("TEST 7: Phase-Guided Two-Hop Retrieval (Logical Composition)")
	console.Info("===========================================================")
	twoHopData := experiment.testTwoHopRetrieval(aphorisms)

	console.Info("\n=============================================================")
	console.Info("TEST 8: Snap-to-Surface Rotational Projection (Manifold Snap)")
	console.Info("=============================================================")
	snapData := experiment.testSnapToSurface(aphorisms)

	console.Info("\n=============================================================")
	console.Info("TEST 9: U(1)×U(1) Torus Navigation (Multi-Axis Phase Sweep)")
	console.Info("=============================================================")
	torusData := experiment.testTorusNavigation(aphorisms)

	console.Info("\n==============================================================")
	console.Info("TEST 10: Torus Generalization (Split Robustness / Overfitting)")
	console.Info("==============================================================")
	genData := experiment.testTorusGeneralization(aphorisms)

	console.Info("\n=============================================================")
	console.Info("TEST 11: Phase Coherence Clustering (Band Structure Analysis)")
	console.Info("=============================================================")
	coherenceData := experiment.testPhaseCoherence(aphorisms)

	report := ValidationReport{
		Seed:          seedValue,
		CorpusHash:    corpusSig,
		BasisHash:     basisSig,
		Candidates:    aphorisms,
		ScanResults:   baselineData,
		TwoHopData:    twoHopData,
		SnapData:      snapData,
		TorusData:     torusData,
		GenData:       genData,
		CoherenceData: coherenceData,
	}

	console.Info("\n=======================================================")
	console.Info("Exporting dynamically generated TeX Section & ECharts figures...")
	console.Info("=======================================================")
	if err := generatePaperOutput(report); err != nil {
		console.Warn(fmt.Sprintf("Failed to generate LaTeX inclusions: %v", err))
	} else {
		console.Info("Successfully generated paper/include/phasedial/phasedial.tex and figures.")
	}

	return nil
}

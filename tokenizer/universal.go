package tokenizer

import (
	"context"
	"math"
	"sync"

	"github.com/theapemachine/six/console"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
)

/*
Token is the byte-level bridge between the geometric and wave domains.
TokenID is the exact replay address; Chord is the wave-space identity used
for matching.
*/
type Token struct {
	SampleID uint32
	TokenID  uint64
	Symbol   byte
	Pos      uint32
	Scale    uint8
	Boundary bool
	Prompt   bool
	Chord    data.Chord
}

type Calibration struct {
	mu               sync.RWMutex
	TargetDensityMin float64
	TargetDensityMax float64
	SensitivityPop   float64
	SensitivityPhase float64
}

func NewCalibration() *Calibration {
	// Base mathematical derivations purely on the Golden Ratio
	// since the spatial chunking logic is FibWindow-based.
	phi := (1.0 + math.Sqrt(5.0)) / 2.0

	return &Calibration{
		// Target densities scale recursively inside the hyperdimensional subspace
		TargetDensityMin: 1.0 / math.Pow(phi, 3), // ~0.236
		TargetDensityMax: 1.0 / math.Pow(phi, 2), // ~0.382

		// Start Z-score thresholds at exactly 1 Golden Ratio standard deviation
		SensitivityPop:   phi,
		SensitivityPhase: phi,
	}
}

/*
Recalibrate is currently a placeholder for future dynamic threshold calibration.
*/
func (calibration *Calibration) Recalibrate() {
	calibration.mu.RLock()
	defer calibration.mu.RUnlock()
}

/*
Sequencer is a mechanism that splits an incoming data stream into sequences
based on the topological properties of the data.
*/
type Sequencer struct {
	calibration *Calibration
	eigen       *geometry.EigenMode
	phi         float64
	phiFast     float64
	phiMed      float64
	emaAlpha    float64
	tokens      []Token
}

func NewSequencer(calibration *Calibration) *Sequencer {
	phi := (1.0 + math.Sqrt(5.0)) / 2.0

	return &Sequencer{
		calibration: calibration,
		eigen:       geometry.NewEigenMode(),
		phi:         phi,
		phiFast:     math.Pow(phi, -9), // ~0.013
		phiMed:      math.Pow(phi, -6), // ~0.055
		emaAlpha:    math.Pow(phi, -3), // ~0.236
	}
}

func (sequencer *Sequencer) Analyze(pos int) (reset bool) {
	// Derive bounds topologically from architecture constants
	topoThresh := math.Pi / config.Numeric.FrequencySpread // Max Phase shift amortized by basis depth

	// Fast and Medium adjustment sensitivities (powers of phi!)
	var emaPop float64 = 0
	var emaPhase float64 = 0
	var coherenceTime int

	// Welford variance accumulators
	var popMean, popM2, phaseMean, phaseM2, count float64

	var windowChord data.Chord

	pop := float64(windowChord.ActiveCount())
	theta, phi := sequencer.eigen.PhaseForChord(&windowChord)
	phase := math.Sqrt(theta*theta + phi*phi)

	if pos == 0 {
		emaPop = pop
		emaPhase = phase
	}

	deltaPop := math.Abs(pop - emaPop)
	deltaPhase := math.Abs(phase - emaPhase)

	// Golden ratio decay smoothing
	emaPop = (emaPop * (1.0 - sequencer.emaAlpha)) + (pop * sequencer.emaAlpha)
	emaPhase = (emaPhase * (1.0 - sequencer.emaAlpha)) + (phase * sequencer.emaAlpha)

	// Continuous stream variance derivation (Welford's online algorithm)
	count++
	popDiff := deltaPop - popMean
	popMean += popDiff / count
	popM2 += popDiff * (deltaPop - popMean)

	phaseDiff := deltaPhase - phaseMean
	phaseMean += phaseDiff / count
	phaseM2 += phaseDiff * (deltaPhase - phaseMean)

	popStdDev := 0.0
	phaseStdDev := 0.0

	if count > 1 {
		popStdDev = math.Sqrt(popM2 / count)
		phaseStdDev = math.Sqrt(phaseM2 / count)
	}

	// The dynamically derived thresholds via Z-scores
	popThresh := popMean + (popStdDev * sequencer.calibration.SensitivityPop)
	phaseThresh := phaseMean + (phaseStdDev * sequencer.calibration.SensitivityPhase)

	if deltaPhase < phaseThresh {
		coherenceTime++
	}

	// Identify true topological boundaries by math, not grammar
	isTopologicalBoundary := deltaPhase > topoThresh

	// Thresholds for natural chunking boundaries
	if deltaPop > popThresh || deltaPhase > phaseThresh {
		var chunkChord data.Chord

		// Feedback 1: Per-primitive density feedback (Fast, continuous)
		density := float64(chunkChord.ActiveCount()) / float64(config.Numeric.ChordBlocks*64)

		if density > sequencer.calibration.TargetDensityMax {
			// Too dense -> Boundaries are too wide -> Decrease threshold multiplier
			sequencer.thresholdMultiplier(-1)
		} else if density < sequencer.calibration.TargetDensityMin {
			// Too sparse -> Boundaries are too narrow -> Increase threshold multiplier
			sequencer.thresholdMultiplier(1)
		}

		if isTopologicalBoundary {
			console.Info(
				"Topological Boundary Detected",
				"sequence", chunkChord,
			)

			reset = true // Reset sequence index on true topological breaks
		}

		coherenceTime = 0
	}

	// Sync back continuous fast updates to the shared calibration state
	// We gently ease back towards base phi using phiMed
	sequencer.calibration.mu.Lock()
	sequencer.calibration.SensitivityPop = sequencer.syncBack(sequencer.calibration.SensitivityPop)
	sequencer.calibration.SensitivityPhase = sequencer.syncBack(sequencer.calibration.SensitivityPhase)
	sequencer.calibration.mu.Unlock()

	return reset
}

func (sequencer *Sequencer) thresholdMultiplier(direction float64) {
	sequencer.calibration.SensitivityPop *= (direction + sequencer.phiFast)
}

func (sequencer *Sequencer) syncBack(element float64) float64 {
	return (element*(1.0-sequencer.phiMed) + element*sequencer.phiMed)
}

/*
FeedbackRetrievalQuality acts as the slow, global supervisor for the tokenizer.
Should be called by BestFill downstream when retrieval performance deviates.
overDiscriminated (true) means we missed relevant matches (false negatives), implying
boundaries are too wide/chaotic.
underDiscriminated (true) means we matched unrelated things (false positives), implying
boundaries aren't detecting enough variance.
*/
func (sequencer *Sequencer) FeedbackRetrievalQuality(overDiscriminated, underDiscriminated bool) {
	sequencer.calibration.mu.Lock()
	defer sequencer.calibration.mu.Unlock()

	phi := (1.0 + math.Sqrt(5.0)) / 2.0
	phiSlow := math.Pow(phi, -5) // ~0.090

	if overDiscriminated {
		// Over-discriminating means too rigidly separating; raise bases to ignore more noise
		sequencer.calibration.SensitivityPop *= (1.0 + phiSlow)
		sequencer.calibration.SensitivityPhase *= (1.0 + phiSlow)
	} else if underDiscriminated {
		// Under-discriminating means not seeing enough fine detail; lower bases to be more sensitive
		sequencer.calibration.SensitivityPop *= (1.0 - phiSlow)
		sequencer.calibration.SensitivityPhase *= (1.0 - phiSlow)
	}
}

/*
Universal converts a byte stream from a Dataset into FibWindow-chunked
tokens. Each position in the stream produces one token per FibWindow scale.
The chord for each chunk is the OR of base chords of all bytes in the window.
*/
type Universal struct {
	ctx       context.Context
	cancel    context.CancelFunc
	coder     *MortonCoder
	dataset   provider.Dataset
	sequencer *Sequencer
	sampleID  uint32
}

type universalOpts func(*Universal)

func NewUniversal(opts ...universalOpts) *Universal {
	tokenizer := &Universal{
		coder:     NewMortonCoder(),
		sequencer: NewSequencer(NewCalibration()),
	}

	for _, opt := range opts {
		opt(tokenizer)
	}

	return tokenizer
}

/*
Generate streams deterministic byte-level tokens.

For the current memorization path, one byte occurrence becomes one Token,
and sample transitions emit explicit boundary markers so the PrimeField can
reset between sequences.
*/
func (tokenizer *Universal) Generate() chan Token {
	out := make(chan Token)

	go func() {
		defer close(out)

		var (
			prevSample uint32
			haveSample bool
		)

		for rawToken := range tokenizer.dataset.Generate() {
			if haveSample && rawToken.SampleID != prevSample {
				out <- Token{
					SampleID: prevSample,
					Boundary: true,
				}
			}

			out <- Token{
				SampleID: rawToken.SampleID,
				TokenID:  tokenizer.coder.Encode(0, rawToken.Pos, rawToken.Symbol),
				Symbol:   rawToken.Symbol,
				Pos:      rawToken.Pos,
				Scale:    0,
				Chord:    data.BaseChord(rawToken.Symbol),
			}

			prevSample = rawToken.SampleID
			haveSample = true
		}

		if haveSample {
			out <- Token{
				SampleID: prevSample,
				Boundary: true,
			}
		}
	}()

	return out
}

func TokenizerWithContext(ctx context.Context) universalOpts {
	return func(tokenizer *Universal) {
		tokenizer.ctx, tokenizer.cancel = context.WithCancel(ctx)
	}
}

func TokenizerWithCoder(coder *MortonCoder) universalOpts {
	return func(tokenizer *Universal) {
		tokenizer.coder = coder
	}
}

func TokenizerWithDataset(dataset provider.Dataset) universalOpts {
	return func(tokenizer *Universal) {
		tokenizer.dataset = dataset
	}
}

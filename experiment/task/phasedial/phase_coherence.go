package phasedial

import (
	"fmt"
	"math"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/numeric"
)

// testPhaseCoherence implements Experiment 11: Phase Coherence Clustering.
//
// Computes the pairwise phase correlation matrix across all corpus fingerprints:
//
//	corr(i,j) = (1/N) Σ_n cos(θ_n_i − θ_n_j)
//
// where θ_n_k = arg(F_n[k]) is the phase angle of dimension k in corpus item n.
//
// If dimensions i and j are "phase-locked" (always carry the same relative
// phase across all corpus items), corr(i,j) ≈ 1. If their phase relationship
// is random, corr(i,j) ≈ 0.
//
// The experiment answers: does the basis prime ordering create contiguous bands
// of phase-coherent dimensions? If yes, this explains why contiguous splits
// enable super-additive composition while random splits don't.
func (experiment *Experiment) testPhaseCoherence(aphorisms []string) CoherenceResult {
	// 1. Encode all corpus items
	N := len(aphorisms)
	D := numeric.NBasis // 512
	fingerprints := make([]numeric.PhaseDial, N)
	for i, text := range aphorisms {
		fingerprints[i] = numeric.EncodeText(text)
	}

	// 2. Extract phases: θ_n_k = atan2(imag, real)
	phases := make([][]float64, N)
	for n := 0; n < N; n++ {
		phases[n] = make([]float64, D)
		for k := 0; k < D; k++ {
			phases[n][k] = math.Atan2(imag(fingerprints[n][k]), real(fingerprints[n][k]))
		}
	}

	console.Info("Computing 512×512 phase correlation matrix...")

	// 3. Compute full D×D phase correlation matrix
	corrMatrix := make([][]float64, D)
	for i := 0; i < D; i++ {
		corrMatrix[i] = make([]float64, D)
	}
	for i := 0; i < D; i++ {
		for j := i; j < D; j++ {
			sum := 0.0
			for n := 0; n < N; n++ {
				sum += math.Cos(phases[n][i] - phases[n][j])
			}
			val := sum / float64(N)
			corrMatrix[i][j] = val
			corrMatrix[j][i] = val // symmetric
		}
	}

	// 4. Downsample to blocks for visualization
	const blockSize = 8
	numBlocks := D / blockSize // 64

	var heatmapData []CoherenceHeatPoint
	for bi := 0; bi < numBlocks; bi++ {
		for bj := 0; bj < numBlocks; bj++ {
			sum := 0.0
			count := 0
			for di := 0; di < blockSize; di++ {
				for dj := 0; dj < blockSize; dj++ {
					i := bi*blockSize + di
					j := bj*blockSize + dj
					if i != j {
						sum += corrMatrix[i][j]
						count++
					}
				}
			}
			val := 0.0
			if count > 0 {
				val = sum / float64(count)
			}
			heatmapData = append(heatmapData, CoherenceHeatPoint{
				X: bi, Y: bj, Value: val,
			})
		}
	}

	// 5. Local coherence profile: mean corr with neighbors within ±window
	const window = 8
	localCoherence := make([]float64, D)
	for k := 0; k < D; k++ {
		sum := 0.0
		count := 0
		for w := -window; w <= window; w++ {
			j := k + w
			if j >= 0 && j < D && j != k {
				sum += corrMatrix[k][j]
				count++
			}
		}
		if count > 0 {
			localCoherence[k] = sum / float64(count)
		}
	}

	// Downsample local coherence to 64 blocks for line chart
	localCoherenceBlocks := make([]float64, numBlocks)
	for bi := 0; bi < numBlocks; bi++ {
		sum := 0.0
		for di := 0; di < blockSize; di++ {
			sum += localCoherence[bi*blockSize+di]
		}
		localCoherenceBlocks[bi] = sum / float64(blockSize)
	}

	// 6. Band correlation analysis: within-band vs between-band mean
	bandCounts := []int{2, 3, 4, 8, 16, 32}
	var bandAnalysis []BandCorrelation

	for _, nBands := range bandCounts {
		bandWidth := D / nBands
		var withinSum, betweenSum float64
		var withinCount, betweenCount int

		for i := 0; i < D; i++ {
			bandI := i / bandWidth
			for j := i + 1; j < D; j++ {
				bandJ := j / bandWidth
				if bandI == bandJ {
					withinSum += corrMatrix[i][j]
					withinCount++
				} else {
					betweenSum += corrMatrix[i][j]
					betweenCount++
				}
			}
		}

		withinMean := 0.0
		if withinCount > 0 {
			withinMean = withinSum / float64(withinCount)
		}
		betweenMean := 0.0
		if betweenCount > 0 {
			betweenMean = betweenSum / float64(betweenCount)
		}
		ratio := 0.0
		if betweenMean != 0 {
			ratio = withinMean / betweenMean
		}

		bandAnalysis = append(bandAnalysis, BandCorrelation{
			NumBands:    nBands,
			BandWidth:   bandWidth,
			WithinMean:  withinMean,
			BetweenMean: betweenMean,
			Ratio:       ratio,
		})

		console.Info(fmt.Sprintf("  %2d bands (w=%3d): within=%.4f  between=%.4f  ratio=%.2f",
			nBands, bandWidth, withinMean, betweenMean, ratio))
	}

	// 7. Report local coherence extrema
	var minLC, maxLC float64 = 1.0, -1.0
	var minIdx, maxIdx int
	for k, v := range localCoherence {
		if v < minLC {
			minLC = v
			minIdx = k
		}
		if v > maxLC {
			maxLC = v
			maxIdx = k
		}
	}
	console.Info(fmt.Sprintf("  Local coherence range: [%.4f at k=%d, %.4f at k=%d]", minLC, minIdx, maxLC, maxIdx))

	// 8. Find natural band boundaries: indices where local coherence drops below a threshold
	meanLC := 0.0
	for _, v := range localCoherence {
		meanLC += v
	}
	meanLC /= float64(D)
	stdLC := 0.0
	for _, v := range localCoherence {
		d := v - meanLC
		stdLC += d * d
	}
	stdLC = math.Sqrt(stdLC / float64(D))

	threshold := meanLC - stdLC
	var boundaries []int
	inLow := false
	for k := 0; k < D; k++ {
		if localCoherence[k] < threshold && !inLow {
			boundaries = append(boundaries, k)
			inLow = true
		} else if localCoherence[k] >= threshold {
			inLow = false
		}
	}
	console.Info(fmt.Sprintf("  Mean local coherence: %.4f ± %.4f", meanLC, stdLC))
	console.Info(fmt.Sprintf("  Natural boundaries (< mean-1σ): %d detected", len(boundaries)))

	// 9. Distance-dependent correlation: C(d) = mean corr(i,j) for |i-j| = d
	console.Info("\n  Distance-dependent correlation C(d):")
	distCorr := make([]float64, D) // C(0) = 1.0 (self), C(1)..C(511)
	distCorr[0] = 1.0
	for dist := 1; dist < D; dist++ {
		sum := 0.0
		count := 0
		for i := 0; i < D-dist; i++ {
			sum += corrMatrix[i][i+dist]
			count++
		}
		distCorr[dist] = sum / float64(count)
	}

	// Report specific distances
	reportDists := []int{1, 2, 4, 8, 16, 32, 64, 128, 256}
	for _, dist := range reportDists {
		if dist < D {
			console.Info(fmt.Sprintf("    C(%3d) = %+.6f", dist, distCorr[dist]))
		}
	}

	// Band means: [1,16], [17,64], [65,256], [257,511]
	type distBand struct {
		dmin, dmax int
	}
	bands := []distBand{{1, 16}, {17, 64}, {65, 256}, {257, 511}}
	var distBandMeans []DistanceBandMean

	console.Info("\n  Distance band means:")
	for _, b := range bands {
		sum := 0.0
		count := 0
		for dist := b.dmin; dist <= b.dmax && dist < D; dist++ {
			sum += distCorr[dist]
			count++
		}
		mean := 0.0
		if count > 0 {
			mean = sum / float64(count)
		}
		distBandMeans = append(distBandMeans, DistanceBandMean{
			DMin: b.dmin, DMax: b.dmax, Mean: mean,
		})
		console.Info(fmt.Sprintf("    d ∈ [%3d, %3d]: mean = %+.6f", b.dmin, b.dmax, mean))
	}

	// Find zero crossing: first d > 1 where C(d) changes sign from negative to positive
	// or from positive to negative
	zeroCrossing := 0
	for dist := 2; dist < D; dist++ {
		if (distCorr[dist-1] < 0 && distCorr[dist] >= 0) ||
			(distCorr[dist-1] >= 0 && distCorr[dist] < 0) {
			zeroCrossing = dist
			break
		}
	}
	if zeroCrossing > 0 {
		console.Info(fmt.Sprintf("\n  First zero crossing: d=%d  (C(%d)=%+.6f → C(%d)=%+.6f)",
			zeroCrossing, zeroCrossing-1, distCorr[zeroCrossing-1], zeroCrossing, distCorr[zeroCrossing]))
	} else {
		console.Info("\n  No zero crossing detected in C(d)")
	}

	return CoherenceResult{
		BlockDimSize:       blockSize,
		NumBlocks:          numBlocks,
		HeatmapData:        heatmapData,
		LocalCoherence:     localCoherenceBlocks,
		BandAnalysis:       bandAnalysis,
		Boundaries:         boundaries,
		MeanLocalCoherence: meanLC,
		StdLocalCoherence:  stdLC,
		DistCorrelation:    distCorr[1:], // C(1)..C(511), omit trivial C(0)=1
		DistBandMeans:      distBandMeans,
		ZeroCrossing:       zeroCrossing,
	}
}

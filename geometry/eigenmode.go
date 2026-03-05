package geometry

import (
	"math"
	"math/cmplx"
	"sort"

	"gonum.org/v1/gonum/mat"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/numeric"
)

/*
EigenMode maps chord structural bins (0–255) to toroidal phases derived from
chord co-occurrence statistics. Co-occurrence is built from chord transitions
within FibWindows—no raw bytes, only chords and TokenIDs.
*/
type EigenMode struct {
	PhaseTheta [256]float64
	PhasePhi   [256]float64
	FreqTheta  [256]float64
	FreqPhi    [256]float64
}

type eigenModeOpts func(*EigenMode)

func NewEigenMode(opts ...eigenModeOpts) *EigenMode {
	ei := &EigenMode{}

	for _, opt := range opts {
		opt(ei)
	}

	return ei
}

/*
BuildMultiScaleCooccurrence generates toroidal phases from chord co-occurrence.
Uses ChordBin to map chords to structural bins 0–255; transition matrix is
built from chord sequences at each FibWindow scale. Chord-native: no raw bytes.
*/
func (ei *EigenMode) BuildMultiScaleCooccurrence(chords []data.Chord) error {
	n := len(chords)
	if n == 0 {
		return nil
	}

	var sinThetaAcc, cosThetaAcc [256]float64
	var sinPhiAcc, cosPhiAcc [256]float64
	var freqThetaAcc, freqPhiAcc [256]float64

	// Generate inverse window scales for FibWeights
	var sum float64
	fibWeights := make([]float64, len(numeric.FibWindows))
	for _, w := range numeric.FibWindows {
		sum += 1.0 / float64(w)
	}
	for i, w := range numeric.FibWindows {
		fibWeights[i] = (1.0 / float64(w)) / sum
	}

	for wi, w := range numeric.FibWindows {
		weight := fibWeights[wi]

		var C [256][256]float64
		ei.buildChordCooccurrenceInto(&C, chords, w)

		vT1, vT2, vP1, vP2, err := ei.toroidalEigenvectors(&C)
		if err != nil {
			return err
		}

		var maxMagTheta, maxMagPhi float64
		magsTheta := make([]float64, 256)
		magsPhi := make([]float64, 256)

		for i := range 256 {
			magsTheta[i] = math.Sqrt(vT1[i]*vT1[i] + vT2[i]*vT2[i])
			if magsTheta[i] > maxMagTheta {
				maxMagTheta = magsTheta[i]
			}
			magsPhi[i] = math.Sqrt(vP1[i]*vP1[i] + vP2[i]*vP2[i])
			if magsPhi[i] > maxMagPhi {
				maxMagPhi = magsPhi[i]
			}
		}

		for i := range 256 {
			phaseTheta := math.Atan2(vT2[i], vT1[i])
			sinThetaAcc[i] += weight * math.Sin(phaseTheta)
			cosThetaAcc[i] += weight * math.Cos(phaseTheta)

			freqTheta := 1.0
			if maxMagTheta > 0 {
				freqTheta = 1.0 + (magsTheta[i]/maxMagTheta)*numeric.FrequencySpread
			}
			freqThetaAcc[i] += weight * freqTheta

			phasePhi := math.Atan2(vP2[i], vP1[i])
			sinPhiAcc[i] += weight * math.Sin(phasePhi)
			cosPhiAcc[i] += weight * math.Cos(phasePhi)

			freqPhi := 1.0
			if maxMagPhi > 0 {
				freqPhi = 1.0 + (magsPhi[i]/maxMagPhi)*numeric.FrequencySpread
			}
			freqPhiAcc[i] += weight * freqPhi
		}
	}

	// Circular mean of the weighted phase contributions for the Torus
	for i := range 256 {
		ei.PhaseTheta[i] = math.Atan2(sinThetaAcc[i], cosThetaAcc[i])
		ei.FreqTheta[i] = freqThetaAcc[i]

		ei.PhasePhi[i] = math.Atan2(sinPhiAcc[i], cosPhiAcc[i])
		ei.FreqPhi[i] = freqPhiAcc[i]
	}

	return nil
}

func (ei *EigenMode) buildChordCooccurrenceInto(C *[256][256]float64, chords []data.Chord, windowSize int) {
	for i := range 256 {
		for j := range 256 {
			C[i][j] = 0
		}
	}

	n := len(chords)
	for pos := 0; pos < n; pos++ {
		binI := data.ChordBin(&chords[pos])
		end := min(pos+windowSize+1, n)
		for j := pos + 1; j < end; j++ {
			binJ := data.ChordBin(&chords[j])
			C[binI][binJ] += 1.0
		}
	}

	// L1-normalize each row -> Markov transition matrix (row sums = 1)
	for i := range 256 {
		var sum float64
		for j := range 256 {
			sum += C[i][j]
		}
		if sum > 0 {
			for j := range 256 {
				C[i][j] /= sum
			}
		}
	}
}

func (ei *EigenMode) toroidalEigenvectors(
	C *[256][256]float64,
) (vT1, vT2, vP1, vP2 [256]float64, err error) {
	data := make([]float64, 256*256)

	for i := range 256 {
		for j := range 256 {
			data[i*256+j] = C[i][j]
		}
	}

	dense := mat.NewDense(256, 256, data)

	var eig mat.Eigen

	if !eig.Factorize(dense, mat.EigenRight) {
		return vT1, vT2, vP1, vP2, console.Error(
			EigenErrorFactorizeFailed,
			"x", 256,
			"y", 256,
		)
	}

	values := eig.Values(nil)
	indices := make([]int, 256)

	for i := range 256 {
		indices[i] = i
	}

	sort.Slice(indices, func(a, b int) bool {
		modA := cmplx.Abs(values[indices[a]])
		modB := cmplx.Abs(values[indices[b]])
		return modA > modB
	})

	var vecs mat.CDense
	eig.VectorsTo(&vecs)

	extractPair := func(idx1, idx2 int, out1, out2 *[256]float64) {
		lam1 := values[idx1]
		lam2 := values[idx2]

		if imag(lam1) != 0 {
			for i := range 256 {
				v := vecs.At(i, idx1)
				out1[i] = real(v)
				out2[i] = imag(v)
			}
		} else if imag(lam2) != 0 {
			for i := range 256 {
				v := vecs.At(i, idx2)
				out1[i] = real(v)
				out2[i] = imag(v)
			}
		} else {
			for i := range 256 {
				out1[i] = real(vecs.At(i, idx1))
				out2[i] = real(vecs.At(i, idx2))
			}
		}
	}

	// Primary subdominant plane (Theta)
	extractPair(indices[1], indices[2], &vT1, &vT2)

	// Secondary subdominant plane (Phi)
	extractPair(indices[3], indices[4], &vP1, &vP2)

	ei.normalizeVec(&vT1)
	ei.normalizeVec(&vT2)
	ei.normalizeVec(&vP1)
	ei.normalizeVec(&vP2)

	return vT1, vT2, vP1, vP2, nil
}

func (ei *EigenMode) normalizeVec(v *[256]float64) {
	var normSq float64
	for i := range 256 {
		normSq += v[i] * v[i]
	}
	if normSq < 1e-12 {
		return
	}
	norm := math.Sqrt(normSq)
	for i := range 256 {
		v[i] /= norm
	}
}

/*
SeqToroidalMeanPhase returns the circular means of eigen phases for a chord sequence.
Chord-native: uses ChordBin to look up phases—no raw bytes.
*/
func (ei *EigenMode) SeqToroidalMeanPhase(chords []data.Chord) (theta, phi float64) {
	n := len(chords)

	if n == 0 {
		return 0, 0
	}

	var sinThetaSum, cosThetaSum float64
	var sinPhiSum, cosPhiSum float64

	for i := range chords {
		bin := data.ChordBin(&chords[i])
		pT := ei.PhaseTheta[bin]
		sinThetaSum += math.Sin(pT)
		cosThetaSum += math.Cos(pT)

		pP := ei.PhasePhi[bin]
		sinPhiSum += math.Sin(pP)
		cosPhiSum += math.Cos(pP)
	}

	return math.Atan2(sinThetaSum, cosThetaSum), math.Atan2(sinPhiSum, cosPhiSum)
}

/*
PhaseForChord returns (theta, phi) for a single chord via ChordBin lookup.
*/
func (ei *EigenMode) PhaseForChord(c *data.Chord) (theta, phi float64) {
	bin := data.ChordBin(c)
	return ei.PhaseTheta[bin], ei.PhasePhi[bin]
}

/*
WeightedCircularMean computes the weighted circular mean and concentration over PhaseTheta 
for a sequence of chords, using FreqTheta as the structural informativeness weights.
*/
func (ei *EigenMode) WeightedCircularMean(chords []data.Chord) (phase float64, concentration float64) {
	if len(chords) == 0 {
		return 0, 0
	}
	var sinSum, cosSum, wSum float64
	for i := range chords {
		bin := data.ChordBin(&chords[i])
		pT := ei.PhaseTheta[bin]
		w := ei.FreqTheta[bin]
		sinSum += w * math.Sin(pT)
		cosSum += w * math.Cos(pT)
		wSum += w
	}
	if wSum == 0 {
		return 0, 0
	}
	phase = math.Atan2(sinSum, cosSum)
	concentration = math.Sqrt(sinSum*sinSum+cosSum*cosSum) / wSum
	return
}

/*
IsGeometricallyClosed verifies structural closure entirely via native
mathematical topology. It checks if the candidate's phase logically
returns to an expected terminal clustered state (proving closure geometrically).
*/
func (ei *EigenMode) IsGeometricallyClosed(chords []data.Chord, anchorPhase float64) bool {
	if len(chords) == 0 {
		return false
	}
	
	cPhase, _ := ei.WeightedCircularMean(chords)
	phaseDiff := math.Abs(cPhase - anchorPhase)
	for phaseDiff > math.Pi {
		phaseDiff = 2*math.Pi - phaseDiff
	}
	
	return phaseDiff < 0.45
}

type EigenError string

const (
	EigenErrorFactorizeFailed EigenError = "eig.Factorize failed"
)

func (err EigenError) Error() string {
	return string(err)
}
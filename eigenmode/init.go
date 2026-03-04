package eigenmode

import (
	"math"
	"math/cmplx"
	"sort"

	"gonum.org/v1/gonum/mat"

	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/store"
)

/*
EigenInit maps each token identity (0–255) to an initial phase angle derived
from the statistical structure of a corpus via eigendecomposition of the
token transition matrix. Co-occurrence is computed at every FibWindow scale.
*/
type EigenInit struct {
	Phase     [256]float64
	Frequency [256]float64
}

type eigenOpts func(*EigenInit)

func NewEigenInit(opts ...eigenOpts) *EigenInit {
	ei := &EigenInit{}

	for _, opt := range opts {
		opt(ei)
	}

	return ei
}

/*
BuildMultiScaleCooccurrence statically generates the continuous structural
manifold phases across all 256 byte symbols directly from the flattened PrimeField.
No concurrency channels or pooling needed!
*/
func (ei *EigenInit) BuildMultiScaleCooccurrence(primefield *store.PrimeField) {
	n := primefield.N
	if n == 0 {
		return
	}

	// 1. Reconstruct the logical byte sequence from Morton keys!
	corpus := make([]byte, n)
	for i := 0; i < n; i++ {
		key := primefield.Key(i)
		// Morton Layout: [8 bits Z | 32 bits Pos (?) | 24 bits Byte] 
		// Actually encode is: (uint64(symbol) << 24) | uint64(pos)
		// Let's decode symbol from >> 24
		corpus[i] = byte((key >> 24) & 0xFF)
	}

	var sinAcc, cosAcc [256]float64
	var freqAcc [256]float64

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
		ei.buildCooccurrenceInto(&C, corpus, w)
		_, v2, v3 := ei.top3Eigenvectors(&C)

		var maxMag float64
		mags := make([]float64, 256)

		for i := 0; i < 256; i++ {
			mags[i] = math.Sqrt(v2[i]*v2[i] + v3[i]*v3[i])
			if mags[i] > maxMag {
				maxMag = mags[i]
			}
		}

		for i := 0; i < 256; i++ {
			phase := math.Atan2(v3[i], v2[i])
			sinAcc[i] += weight * math.Sin(phase)
			cosAcc[i] += weight * math.Cos(phase)

			freq := 1.0
			if maxMag > 0 {
				freq = 1.0 + (mags[i]/maxMag)*9.0 // 9.0 is FrequencySpread
			}

			freqAcc[i] += weight * freq
		}
	}

	// Circular mean of the weighted phase contributions
	for i := 0; i < 256; i++ {
		ei.Phase[i] = math.Atan2(sinAcc[i], cosAcc[i])
		ei.Frequency[i] = freqAcc[i]
	}
}

func (ei *EigenInit) buildCooccurrenceInto(C *[256][256]float64, corpus []byte, windowSize int) {
	for i := 0; i < 256; i++ {
		for j := 0; j < 256; j++ {
			C[i][j] = 0
		}
	}

	n := len(corpus)
	for pos, sym := range corpus {
		end := pos + windowSize + 1
		if end > n {
			end = n
		}
		for j := pos + 1; j < end; j++ {
			otherSym := corpus[j]
			C[sym][otherSym] += 1.0
		}
	}

	// L1-normalize each row -> Markov transition matrix (row sums = 1)
	for i := 0; i < 256; i++ {
		var sum float64
		for j := 0; j < 256; j++ {
			sum += C[i][j]
		}
		if sum > 0 {
			for j := 0; j < 256; j++ {
				C[i][j] /= sum
			}
		}
	}
}

func (ei *EigenInit) top3Eigenvectors(C *[256][256]float64) (v1, v2, v3 [256]float64) {
	data := make([]float64, 256*256)
	for i := 0; i < 256; i++ {
		for j := 0; j < 256; j++ {
			data[i*256+j] = C[i][j]
		}
	}
	dense := mat.NewDense(256, 256, data)

	var eig mat.Eigen
	if !eig.Factorize(dense, mat.EigenRight) {
		panic("eigenmode: eig.Factorize failed")
	}

	values := eig.Values(nil)
	indices := make([]int, 256)
	for i := 0; i < 256; i++ {
		indices[i] = i
	}

	sort.Slice(indices, func(a, b int) bool {
		modA := cmplx.Abs(values[indices[a]])
		modB := cmplx.Abs(values[indices[b]])
		return modA > modB
	})

	var vecs mat.CDense
	eig.VectorsTo(&vecs)

	idx1, idx2 := indices[1], indices[2]
	lam1 := values[idx1]
	lam2 := values[idx2]

	if imag(lam1) != 0 {
		for i := 0; i < 256; i++ {
			v := vecs.At(i, idx1)
			v2[i] = real(v)
			v3[i] = imag(v)
		}
	} else if imag(lam2) != 0 {
		for i := 0; i < 256; i++ {
			v := vecs.At(i, idx2)
			v2[i] = real(v)
			v3[i] = imag(v)
		}
	} else {
		for i := 0; i < 256; i++ {
			v2[i] = real(vecs.At(i, idx1))
			v3[i] = real(vecs.At(i, idx2))
		}
	}

	for i := 0; i < 256; i++ {
		v1[i] = real(vecs.At(i, indices[0]))
	}
	var v1Sum float64
	for i := 0; i < 256; i++ {
		v1Sum += v1[i]
	}
	if v1Sum < 0 {
		for i := 0; i < 256; i++ {
			v1[i] = -v1[i]
		}
	}

	ei.normalizeVec(&v1)
	ei.normalizeVec(&v2)
	ei.normalizeVec(&v3)
	return v1, v2, v3
}

func (ei *EigenInit) normalizeVec(v *[256]float64) {
	var normSq float64
	for i := 0; i < 256; i++ {
		normSq += v[i] * v[i]
	}
	if normSq < 1e-12 {
		return
	}
	norm := math.Sqrt(normSq)
	for i := 0; i < 256; i++ {
		v[i] /= norm
	}
}

/*
SeqCircularMeanPhase returns the circular mean of the eigen phases of each byte in seq.
*/
func (ei *EigenInit) SeqCircularMeanPhase(seq []byte) float64 {
	n := len(seq)
	if n == 0 {
		return 0
	}
	var sinSum, cosSum float64
	for _, b := range seq {
		p := ei.Phase[b]
		sinSum += math.Sin(p)
		cosSum += math.Cos(p)
	}
	return math.Atan2(sinSum, cosSum)
}

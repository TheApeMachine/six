package geometry

import (
	"context"
	"fmt"
	"math"
	"math/cmplx"
	"sort"

	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/tphakala/simd/f64"
	"gonum.org/v1/gonum/mat"
)

/*
FrequencySpread is the number of octaves to spread the frequency across.
*/
var FrequencySpread = math.Log2(float64(512))

/*
propertyWords is the number of uint64 words in the canonical 512-bit properties region
(words 48–55 on the Value layout).
*/
const propertyWords = 8

/*
phaseScalarSumThresholdLog2 is the inclusive byte-count ceiling for the allocation-free
scalar sin/cos accumulation path in SeqCircularMeanPhaseFromPhases (matches log2(512)).
*/
const phaseScalarSumThresholdLog2 = 9

/*
symbolFromPropertyWord maps one 64-bit lane to a matrix index 0…511. Callers that hold a
full properties snapshot should fold first (e.g. SymbolFromPropertyBand).
*/
func symbolFromPropertyWord(word uint64) int {
	return int(word & 511)
}

/*
SymbolFromPropertyBand folds the eight-word properties region into one 0…511 symbol for
transition statistics (xor-mix, then low 9 bits). Same symbol used for co-occurrence rows.
*/
func SymbolFromPropertyBand(words []uint64) int {
	wordCount := len(words)

	if wordCount == 0 {
		return 0
	}

	if wordCount > propertyWords {
		wordCount = propertyWords
	}

	var mix uint64

	for index := 0; index < wordCount; index++ {
		word := words[index]
		mix ^= word
		mix ^= word >> 32
	}

	return int(mix & 511)
}

/*
EigenModeToroidal

Maps oscillators to an initial phase angle from the forward transition
statistics of **property symbols** (512×512 Markov matrix), via eigendecomposition.
Rows/columns index 0…511 — the same width as the properties band (8×64 bits) used
as discrete tags for forward potential / eigenmode phases (see README “Properties”).

Co-occurrence is computed at every FibWindow scale.

Why this matters:
  - Phase cannot start at 0 for all degrees of freedom — that collapses the system trivially.
  - Phase cannot come from sequence index — that pre-imposes structure.
  - Phase must emerge from observed **what-follows-what** structure in the symbol stream.

The transition matrix T encodes that: T[i][j] counts how often symbol i PRECEDES symbol j
within a window. Asymmetric — T[i][j] ≠ T[j][i] — the Arrow of Time: leading vs lagging
phases in the (v2,v3) eigenplane.

Algorithm:
 1. Build asymmetric 512×512 forward transition matrix T per FibWindow.
 2. Extract top 3 eigenvectors via gonum/mat.Eigen.
 3. Skip v1 (all-positive by Perron-Frobenius).
 4. Phase[i] = atan2(v3[i], v2[i]). Combine across scales via weighted circular mean.
*/
type EigenModeToroidal struct {
	ctx       context.Context
	cancel    context.CancelFunc
	phaser    *Phase
	phase     [512]float64
	frequency []float64
	affinity  []uint64
	err       error
}

type eigenOpts func(*EigenModeToroidal)

func NewEigenModeToroidal(opts ...eigenOpts) (*EigenModeToroidal, error) {
	emt := &EigenModeToroidal{
		phase:     [512]float64{},
		frequency: []float64{},
		phaser:    NewPhase(),
	}

	for _, opt := range opts {
		opt(emt)
	}

	if err := validate.Require(map[string]any{
		"ctx":      emt.ctx,
		"cancel":   emt.cancel,
		"affinity": emt.affinity,
		"phaser":   emt.phaser,
	}); err != nil {
		return nil, fmt.Errorf("eigenmode toroidal: %w", err)
	}

	return emt, nil
}

/*
buildMultiScaleCooccurrence superimposes Fibonacci-windowed co-occurrence
eigen phases. For each window w in fibWindows, it computes the (v2, v3)
eigenplane and derives Phase[i] and Frequency[i]. The final Phase is the
weighted circular mean across all scales; Frequency is the weighted sum.

Smaller windows → fine-grained local correlation.
Larger windows  → longer-range coupling.
The composite starting state gives every oscillator knowledge of both scales.
*/
func (emt *EigenModeToroidal) buildMultiScaleCooccurrence(corpus []uint64) {
	sinAcc := make([]float64, 512)
	cosAcc := make([]float64, 512)
	freqAcc := make([]float64, 512)

	for wi, w := range numeric.FibWindows {
		weight := numeric.FibWeights[wi]

		var C [512][512]float64
		emt.buildCooccurrenceInto(&C, corpus, w)
		_, v2, v3 := emt.top3Eigenvectors(&C)

		// Magnitude in (v2,v3) eigenplane → per-scale frequency contribution.
		var maxMag float64
		mags := make([]float64, 512)

		for i := range 512 {
			mags[i] = math.Sqrt(v2[i]*v2[i] + v3[i]*v3[i])

			if mags[i] > maxMag {
				maxMag = mags[i]
			}
		}

		for i := range 512 {
			phase := math.Atan2(v3[i], v2[i])
			sinAcc[i] += weight * math.Sin(phase)
			cosAcc[i] += weight * math.Cos(phase)

			freq := 1.0

			if maxMag > 0 {
				freq = 1.0 + (mags[i]/maxMag)*math.Log2(512.0)
			}

			freqAcc[i] += weight * freq
		}
	}

	// Circular mean of the weighted phase contributions.
	for i := range 512 {
		emt.phase[i] = math.Atan2(sinAcc[i], cosAcc[i])
		emt.frequency[i] = freqAcc[i]
	}
}

/*
BuildCooccurrence builds the co-occurrence matrix and computes the top 3 eigenvectors.

Each input byte is mapped to a symbol 0…255 via its numeric value (same index family as
SeqCircularMeanPhase). For arbitrary uint64 tags per timestep, use BuildCooccurrenceFromWords.
*/
func (emt *EigenModeToroidal) BuildCooccurrence(corpus []byte, windowSize int) {
	tags := make([]uint64, len(corpus))

	for index, b := range corpus {
		tags[index] = uint64(b)
	}

	emt.BuildCooccurrenceFromWords(tags, windowSize)
}

/*
BuildCooccurrenceFromWords is the same Markov lift as BuildCooccurrence, but each timestep is
already a uint64 tag (e.g. one lane or a pre-mixed property key). Symbols are 0…511 via
symbolFromPropertyWord.
*/
func (emt *EigenModeToroidal) BuildCooccurrenceFromWords(tags []uint64, windowSize int) {
	if len(emt.frequency) < 512 {
		emt.frequency = make([]float64, 512)
	}

	var C [512][512]float64

	emt.buildCooccurrenceInto(&C, tags, windowSize)

	_, v2, v3 := emt.top3Eigenvectors(&C)

	var v2sq, v3sq, mags [512]float64

	f64.Mul(v2sq[:], v2[:], v2[:])
	f64.Mul(v3sq[:], v3[:], v3[:])
	f64.Add(mags[:], v2sq[:], v3sq[:])
	f64.Sqrt(mags[:], mags[:])

	maxMag := f64.Max(mags[:])

	f64.Atan2(emt.phase[:], v3[:], v2[:])

	if maxMag > 0 {
		f64.Scale(emt.frequency[:], mags[:], FrequencySpread/maxMag)
		f64.AddScalar(emt.frequency[:], emt.frequency[:], 1.0)
	} else {
		for i := range 512 {
			emt.frequency[i] = 1.0
		}
	}
}

/*
buildCooccurrenceInto fills C with the asymmetric forward transition matrix.
T[i][j] counts how often symbol i PRECEDES symbol j within windowSize positions.
Only forward neighbors (j > pos) are counted, making T asymmetric.

Rows are L1-normalized (sum = 1) so C behaves as a Markov transition matrix.
*/
func (emt *EigenModeToroidal) buildCooccurrenceInto(
	C *[512][512]float64,
	corpus []uint64,
	windowSize int,
) {
	for i := range 512 {
		for j := range 512 {
			C[i][j] = 0
		}
	}

	for pos := range corpus {
		sym := symbolFromPropertyWord(corpus[pos])
		end := min(len(corpus), pos+windowSize+1)

		for j := pos + 1; j < end; j++ {
			otherSym := symbolFromPropertyWord(corpus[j])
			C[sym][otherSym] += 1.0
		}
	}
	// L1-normalize each row → Markov transition matrix (row sums = 1).
	for i := range 512 {
		sum := f64.Sum(C[i][:])
		if sum > 0 {
			f64.Scale(C[i][:], C[i][:], 1.0/sum)
		}
	}
}

/*
top3Eigenvectors returns the top 3 eigenvectors of the transition matrix
using gonum/mat.Eigen. Robust for asymmetric matrices, including those with
complex conjugate eigenvalue pairs. Only v2 and v3 are used for phase mapping
— v1 is all-positive (Perron-Frobenius) and carries no angular info.
*/
func (emt *EigenModeToroidal) top3Eigenvectors(
	C *[512][512]float64,
) (v1, v2, v3 [512]float64) {
	// Build row-major dense matrix for gonum
	data := make([]float64, 512*512)
	for rowIdx := range 512 {
		for colIdx := range 512 {
			data[rowIdx*512+colIdx] = C[rowIdx][colIdx]
		}
	}
	dense := mat.NewDense(512, 512, data)

	var eig mat.Eigen
	if !eig.Factorize(dense, mat.EigenRight) {
		v1, v2, v3 = emt.top3EigenvectorsPowerIteration(C)
		return emt.alignAndNormalizeTop3(&v1, &v2, &v3)
	}

	values := eig.Values(nil)
	indices := make([]int, 512)
	for i := range 512 {
		indices[i] = i
	}
	sort.Slice(indices, func(a, b int) bool {
		modA := cmplx.Abs(values[indices[a]])
		modB := cmplx.Abs(values[indices[b]])
		return modA > modB
	})

	var vecs mat.CDense
	eig.VectorsTo(&vecs)

	// idx0 ≈ Perron (λ≈1), idx1 and idx2 are next by magnitude
	idx1, idx2 := indices[1], indices[2]
	lam1 := values[idx1]
	lam2 := values[idx2]

	// Extract real vectors for (v2, v3) eigenplane
	if imag(lam1) != 0 {
		// idx1 and idx2 are complex conjugates; use real and imag of column idx1
		for rowIdx := range 512 {
			eigenComponent := vecs.At(rowIdx, idx1)
			v2[rowIdx] = real(eigenComponent)
			v3[rowIdx] = imag(eigenComponent)
		}
	} else if imag(lam2) != 0 {
		// idx1 real, idx2 complex: 2D invariant subspace from real/imag of same eigenvector
		for rowIdx := range 512 {
			eigenComponent := vecs.At(rowIdx, idx2)
			v2[rowIdx] = real(eigenComponent)
			v3[rowIdx] = imag(eigenComponent)
		}
	} else {
		// Both real
		for rowIdx := range 512 {
			v2[rowIdx] = real(vecs.At(rowIdx, idx1))
			v3[rowIdx] = real(vecs.At(rowIdx, idx2))
		}
	}

	// v1 from leading (Perron) eigenvector. Sign is arbitrary; flip to all-non-negative.
	for rowIdx := range 512 {
		v1[rowIdx] = real(vecs.At(rowIdx, indices[0]))
	}

	return emt.alignAndNormalizeTop3(&v1, &v2, &v3)
}

/*
alignAndNormalizeTop3 flips the leading eigenvector sign to nonnegative sum, then L2-normalizes
all three vectors returned for phase mapping.
*/
func (emt *EigenModeToroidal) alignAndNormalizeTop3(v1, v2, v3 *[512]float64) (v1out, v2out, v3out [512]float64) {
	var v1Sum float64

	for rowIdx := range 512 {
		v1Sum += v1[rowIdx]
	}

	if v1Sum < 0 {
		for rowIdx := range 512 {
			v1[rowIdx] = -v1[rowIdx]
		}
	}

	emt.normalizeVec(v1)
	emt.normalizeVec(v2)
	emt.normalizeVec(v3)

	return *v1, *v2, *v3
}

/*
top3EigenvectorsPowerIteration is the legacy power-iteration path, used when
gonum factorization fails or as a fallback for degenerate matrices.
*/
func (emt *EigenModeToroidal) top3EigenvectorsPowerIteration(
	C *[512][512]float64,
) (v1, v2, v3 [512]float64) {
	v1, lam1 := emt.powerIterate(C, emt.uniformStart())
	u1 := emt.powerIterateLeft(C, emt.uniformStart())

	C2 := emt.deflateBiorthogonal(C, &u1, &v1, lam1)
	v2, lam2 := emt.powerIterate(&C2, emt.sawtoothStart())
	u2 := emt.powerIterateLeft(&C2, emt.sawtoothStart())

	C3 := emt.deflateBiorthogonal(&C2, &u2, &v2, lam2)
	v3, _ = emt.powerIterate(&C3, emt.cosineStart())

	return v1, v2, v3
}

/*
powerIterate runs power iteration on M until convergence or maxIter steps.
Returns the dominant right eigenvector and its eigenvalue (Rayleigh quotient).
*/
func (emt *EigenModeToroidal) powerIterate(
	M *[512][512]float64, v [512]float64,
) ([512]float64, float64) {
	const maxIter = 2000
	const tol = 1e-10

	emt.normalizeVec(&v)
	var lambda float64

	for range maxIter {
		var mv [512]float64
		for i := range 512 {
			mv[i] = f64.DotProduct(M[i][:], v[:])
		}
		newLambda := f64.DotProduct(v[:], mv[:])

		normSq := f64.SumOfSquares(mv[:])
		if normSq < 1e-24 {
			break
		}
		norm := math.Sqrt(normSq)
		f64.Scale(mv[:], mv[:], 1.0/norm)

		if math.Abs(newLambda-lambda) < tol {
			return mv, newLambda
		}
		v = mv
		lambda = newLambda
	}
	return v, lambda
}

/*
powerIterateLeft runs power iteration on Mᵀ to find the dominant left eigenvector u.
uᵀ M = λ uᵀ  ⟺  Mᵀ u = λ u. Matrix-vector product: (Mᵀ u)_i = Σ_j M[j][i] u[j].
*/
func (emt *EigenModeToroidal) powerIterateLeft(
	M *[512][512]float64, start [512]float64,
) [512]float64 {
	const maxIter = 2000
	const tol = 1e-10

	u := start
	emt.normalizeVec(&u)
	var lambda float64

	for range maxIter {
		var Mu [512]float64
		for i := range 512 {
			var sum float64
			for j := range 512 {
				sum += M[j][i] * u[j]
			}
			Mu[i] = sum
		}
		newLambda := f64.DotProduct(u[:], Mu[:])

		normSq := f64.SumOfSquares(Mu[:])
		if normSq < 1e-24 {
			break
		}
		norm := math.Sqrt(normSq)
		f64.Scale(Mu[:], Mu[:], 1.0/norm)

		if math.Abs(newLambda-lambda) < tol {
			return Mu
		}
		u = Mu
		lambda = newLambda
	}
	return u
}

/*
deflateBiorthogonal removes the rank-1 component corresponding to right eigenvector v
and left eigenvector u with eigenvalue λ. For asymmetric M, the correct deflation is
M_new = M - λ v uᵀ / (uᵀ v). Element: D[i][j] = M[i][j] - λ v[i] u[j] / dot(u,v).
*/
func (emt *EigenModeToroidal) deflateBiorthogonal(
	M *[512][512]float64,
	u, v *[512]float64,
	lam float64,
) [512][512]float64 {
	uTv := f64.DotProduct(u[:], v[:])

	if math.Abs(uTv) < 1e-10 {
		// Degenerate: return M unchanged.
		return *M
	}

	scale := lam / uTv

	var D [512][512]float64

	for i := range 512 {
		for j := range 512 {
			D[i][j] = M[i][j] - scale*v[i]*u[j]
		}
	}

	return D
}

// uniformStart returns a unit-norm vector with all equal components.
func (emt *EigenModeToroidal) uniformStart() [512]float64 {
	var v [512]float64

	val := 1.0 / math.Sqrt(float64(512))

	for i := range 512 {
		v[i] = val
	}

	return v
}

// sawtoothStart returns a sawtooth vector for finding the 2nd eigenvector.
func (emt *EigenModeToroidal) sawtoothStart() [512]float64 {
	var (
		v                [512]float64
		intervalMidpoint = 0.5
	)

	for i := range 512 {
		v[i] = float64(i)/float64(512) - intervalMidpoint
	}

	emt.normalizeVec(&v)
	return v
}

// cosineStart returns a cosine-shaped vector for finding the 3rd eigenvector.
func (emt *EigenModeToroidal) cosineStart() [512]float64 {
	var v [512]float64

	for i := range 512 {
		v[i] = 2 * math.Pi * float64(i) / float64(512)
	}

	f64.Cos(v[:], v[:])
	emt.normalizeVec(&v)

	return v
}

/*
SeqCircularMeanPhase returns the circular mean of the eigen phases of each
byte in seq. Two sequences sharing common bytes get nearby phases, so contexts
built from similar characters cluster together in eigen phase space.

For short sequences, uses scalar loop to avoid slice allocations.
*/
func (emt *EigenModeToroidal) SeqCircularMeanPhase(seq []byte) (float64, error) {
	return SeqCircularMeanPhaseFromPhases(&emt.phase, seq)
}

/*
SeqCircularMeanPhaseFromPhases returns the circular mean of phases for bytes in seq.
Consumers that receive PhasesSnapshot from the eigen group use this. Shared logic.
*/
func SeqCircularMeanPhaseFromPhases(phase *[512]float64, seq []byte) (float64, error) {
	n := len(seq)
	if n == 0 {
		return 0, EigenErrorEmptySequence
	}
	if n <= phaseScalarSumThresholdLog2 {
		var sinSum, cosSum float64
		for _, b := range seq {
			p := phase[b]
			sinSum += math.Sin(p)
			cosSum += math.Cos(p)
		}
		return math.Atan2(sinSum, cosSum), nil
	}
	phases := make([]float64, n)
	for i, b := range seq {
		phases[i] = phase[b]
	}
	sinBuf := make([]float64, n)
	cosBuf := make([]float64, n)
	f64.SinCos(sinBuf, cosBuf, phases)
	return math.Atan2(f64.Sum(sinBuf), f64.Sum(cosBuf)), nil
}

func (emt *EigenModeToroidal) normalizeVec(v *[512]float64) {
	normSq := f64.SumOfSquares(v[:])

	if normSq < 1e-10 {
		return
	}

	norm := math.Sqrt(normSq)
	f64.Scale(v[:], v[:], 1.0/norm)
}

func EigenWithContext(ctx context.Context) eigenOpts {
	return func(emt *EigenModeToroidal) {
		emt.ctx = ctx
	}
}

type EigenError string

const (
	EigenErrorEmptySequence EigenError = "sequence is empty"
)

func (e EigenError) Error() string {
	return string(e)
}

package tokenizer

import (
	"math"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
Sequencer discovers natural boundaries in a raw byte stream using the
Minimum Description Length (MDL) principle.
*/
type Sequencer struct {
	calibrator *Calibrator
	eigen      *geometry.EigenMode
	phi        float64

	buf  []byte
	dist *Distribution

	prevSegLen  int
	fluxEmitted bool

	lastByteVal  float64
	lastEigenMag float64

	emaPhase float64
	emaPop   float64

	tokens     []Token
	candidates []candidate
	offset     int

	// MinSegmentBytes: minimum bytes per segment for statistical
	// significance. Default 4; increase for noisier streams.
	MinSegmentBytes int
}

type candidate struct {
	k       int
	gain    float64 // penalized MDL gain; used as confidence for emission
	entropy float64 // entropy jump at split (optional extra evidence)
}

/*
NewSequencer creates a Sequencer with optional calibrator for BIC penalty tuning.
Default MinSegmentBytes=4.
*/
func NewSequencer(calibrator *Calibrator) *Sequencer {
	return &Sequencer{
		calibrator:      calibrator,
		eigen:           geometry.NewEigenMode(),
		phi:             (1.0 + math.Sqrt(5.0)) / 2.0,
		dist:            NewDistribution(),
		MinSegmentBytes: 4,
	}
}

/*
CloneEmpty returns a new Sequencer with the same config (calibrator, eigen, phi)
but empty buffer, distribution, and candidates.
*/
func (seq *Sequencer) CloneEmpty() *Sequencer {
	return &Sequencer{
		calibrator:      seq.calibrator,
		eigen:           seq.eigen,
		phi:             seq.phi,
		dist:            NewDistribution(),
		MinSegmentBytes: seq.MinSegmentBytes,
	}
}

/*
Clone returns a deep copy of the Sequencer including buffer, dist, candidates, and offset.
*/
func (seq *Sequencer) Clone() *Sequencer {
	c := seq.CloneEmpty()
	c.buf = append([]byte(nil), seq.buf...)
	c.dist = seq.dist.Clone()
	c.prevSegLen = seq.prevSegLen
	c.fluxEmitted = seq.fluxEmitted
	c.lastByteVal = seq.lastByteVal
	c.lastEigenMag = seq.lastEigenMag
	c.emaPhase = seq.emaPhase
	c.emaPop = seq.emaPop
	c.tokens = append([]Token(nil), seq.tokens...)
	c.candidates = append([]candidate(nil), seq.candidates...)
	c.offset = seq.offset
	return c
}

func slog(c int) float64 {
	if c <= 0 {
		return 0
	}
	return float64(c) * math.Log(float64(c))
}

/*
Analyze appends byteVal, runs MDL boundary detection, and optionally emits a split.
Returns (true, events) when a boundary is committed; (false, events) otherwise.
events are geometry.Event* constants for PrimeField.Rotate.
*/
func (seq *Sequencer) Analyze(pos int, byteVal byte) (bool, []int) {
	val, delta, eigenMag := seq.computeSignal(byteVal)

	seq.buf = append(seq.buf, byteVal)
	seq.dist.Add(byteVal)

	isBoundary, k, gain := seq.detectBoundary(seq.buf[seq.offset:], seq.dist)
	var events []int

	if isBoundary {
		absK := seq.offset + k
		seq.candidates = append(seq.candidates, candidate{k: absK, gain: gain})
		seq.offset = absK

		// Reset distribution for the next search window.
		// Any bytes added to seq.buf AFTER absK should be in the new distribution.
		seq.dist = NewDistribution()
		for _, b := range seq.buf[seq.offset:] {
			seq.dist.Add(b)
		}
	}

	if len(seq.candidates) >= 2 {
		seq.balanceCandidates()
	}

	// Emit when we have a stable sequence of candidates.
	// We wait for 2 candidates to ensure the first one is well-balanced.
	if len(seq.candidates) >= 2 {
		emitK := seq.candidates[0].k

		events = append(events, seq.classifyDirection(seq.buf, emitK))
		events = append(events, geometry.EventPhaseInversion)

		seq.emitSplit(emitK)

		// Shift all remaining candidates and the offset.
		seq.candidates = seq.candidates[1:]

		for i := range seq.candidates {
			seq.candidates[i].k -= emitK
		}

		seq.offset -= emitK
		seq.updateEMA(val, delta, eigenMag)

		if seq.calibrator != nil {
			seq.calibrator.Recalibrate(events)
		}

		return true, events
	}

	if seq.hasFlux() {
		events = append(events, geometry.EventLowVarianceFlux)
		seq.fluxEmitted = true
	}

	seq.updateEMA(val, delta, eigenMag)
	if seq.calibrator != nil {
		seq.calibrator.Recalibrate(events)
	}
	return false, events
}

func (seq *Sequencer) hasFlux() bool {
	return seq.prevSegLen > 0 && (len(seq.buf)-seq.offset) >= seq.prevSegLen && !seq.fluxEmitted
}

func (seq *Sequencer) computeSignal(byteVal byte) (val, delta, eigenMag float64) {
	val = float64(byteVal)

	if seq.eigen != nil && seq.eigen.Trained {
		c := data.BaseChord(byteVal)
		theta, phi := seq.eigen.PhaseForChord(&c)
		eigenMag = math.Hypot(theta, phi)
		delta = math.Abs(eigenMag - seq.lastEigenMag)
		return
	}

	delta = math.Abs(val - seq.lastByteVal)
	return
}

func (seq *Sequencer) detectBoundary(buf []byte, dist *Distribution) (bool, int, float64) {
	n := len(buf)
	minSeg := max(seq.MinSegmentBytes, 2)

	if n < 2*minSeg {
		return false, 0, 0
	}

	penaltyScale := 1.0

	if seq.calibrator != nil && seq.calibrator.sensitivityPop > 0 {
		penaltyScale = seq.calibrator.sensitivityPop
	}

	bestK := 0
	maxGain := 0.0

	costFull := dist.Cost()
	left := NewDistribution()
	right := NewDistribution()
	*right = *dist // fast copy of histogram state

	for i := 1; i < n; i++ {
		b := buf[i-1]

		left.Add(b)
		right.Remove(b)

		// Guard: segments must have enough evidence to be statistically significant.
		if i < minSeg || n-i < minSeg {
			continue
		}

		gain := costFull - (left.Cost() + right.Cost())

		// BIC-like penalty: reflect both submodels.
		// Multinomial has (numDistinct-1) free parameters per side.
		leftParams := max(left.numDistinct-1, 1)
		rightParams := max(right.numDistinct-1, 1)
		smallerN := min(n-i, i)
		penalty := penaltyScale * 0.5 * float64(leftParams+rightParams) * math.Log(float64(smallerN))

		if gain-penalty > maxGain {
			maxGain = gain - penalty
			bestK = i
		}
	}

	return maxGain > 0, bestK, maxGain
}

func (seq *Sequencer) emitSplit(k int) {
	seq.prevSegLen = k
	seq.buf = append([]byte(nil), seq.buf[k:]...) // force copy to avoid leaks
	seq.fluxEmitted = false
}

func (seq *Sequencer) balanceCandidates() {
	if len(seq.candidates) < 2 {
		return
	}

	c1 := &seq.candidates[0]
	c2 := &seq.candidates[1]

	combinedBuf := seq.buf[:c2.k]
	jointDist := seq.getDistribution(0, c2.k)

	found, bestK, gain := seq.detectBoundary(combinedBuf, jointDist)
	if !found {
		seq.candidates = seq.candidates[1:]
		return
	}

	// Similarity check: if left and right are nearly identical distributions,
	// the split is likely spurious — merge and keep c2 as the outer boundary.
	d1 := seq.getDistribution(0, bestK)
	d2 := seq.getDistribution(bestK, c2.k)
	if seq.isSimilar(d1, d2) {
		seq.candidates = seq.candidates[1:]
		return
	}

	c1.k = bestK
	c1.gain = gain
}

func (seq *Sequencer) getDistribution(start, end int) *Distribution {
	dist := NewDistribution()
	for _, b := range seq.buf[start:end] {
		dist.Add(b)
	}
	return dist
}

func (seq *Sequencer) isSimilar(d1, d2 *Distribution) bool {
	if d1.n == 0 || d2.n == 0 {
		return false
	}

	c1 := d1.Cost() / float64(d1.n)
	c2 := d2.Cost() / float64(d2.n)

	return math.Abs(c1-c2) < 0.2
}

/*
classifyDirection returns EventDensitySpike if right-mean > left-mean, else EventDensityTrough.
Compares buf[:k] vs buf[k:] mean byte values.
*/
func (seq *Sequencer) classifyDirection(buf []byte, k int) int {
	if k <= 0 || k >= len(buf) {
		return geometry.EventDensityTrough
	}
	var leftSum, rightSum int
	for _, b := range buf[:k] {
		leftSum += int(b)
	}
	for _, b := range buf[k:] {
		rightSum += int(b)
	}
	leftMean := float64(leftSum) / float64(k)
	rightMean := float64(rightSum) / float64(len(buf)-k)
	if rightMean > leftMean {
		return geometry.EventDensitySpike
	}
	return geometry.EventDensityTrough
}

func (seq *Sequencer) updateEMA(val, delta, eigenMag float64) {
	seq.lastByteVal = val
	if seq.eigen != nil && seq.eigen.Trained {
		seq.lastEigenMag = eigenMag
	}
	n := max(len(seq.buf), 1)
	alpha := 1.0 / float64(n)
	seq.emaPop = seq.emaPop*(1-alpha) + val*alpha
	seq.emaPhase = seq.emaPhase*(1-alpha) + delta*alpha
}

/*
Forecast runs boundary detection on buf+byteVal without committing.
Returns (true, events) if a boundary would be at k; (false, nil) otherwise.
*/
func (seq *Sequencer) Forecast(pos int, byteVal byte) (bool, []int) {
	buf := append(seq.buf, byteVal)
	dist := *seq.dist
	dist.Add(byteVal)

	window := buf[seq.offset:]
	isBoundary, k, _ := seq.detectBoundary(window, &dist)
	if !isBoundary {
		return false, nil
	}

	// Classify using only the active window, not full buffer history.
	events := append([]int{}, seq.classifyDirection(window, k))
	events = append(events, geometry.EventPhaseInversion)
	return true, events
}

/*
FeedbackRetrievalQuality adjusts calibrator.sensitivityPop by 1/phi.
overDiscriminated: increase penalty (fewer splits). underDiscriminated: decrease.
Clamps sensitivityPop to [0.05, 20].
*/
func (seq *Sequencer) FeedbackRetrievalQuality(overDiscriminated, underDiscriminated bool) {
	if seq.calibrator == nil {
		return
	}
	seq.calibrator.mu.Lock()
	defer seq.calibrator.mu.Unlock()
	adjust := 1.0 / seq.phi
	if overDiscriminated {
		seq.calibrator.sensitivityPop *= (1.0 + adjust)
	} else if underDiscriminated {
		seq.calibrator.sensitivityPop *= (1.0 - adjust)
	}
	seq.calibrator.sensitivityPop = math.Max(0.05, math.Min(20.0, seq.calibrator.sensitivityPop))
}

/*
Phase returns (emaPhase, costPerByte). costPerByte = dist.Cost()/n.
*/
func (seq *Sequencer) Phase() (float64, float64) {
	return seq.emaPhase, seq.dist.Cost() / float64(max(seq.dist.n, 1))
}

/*
Phi returns the golden-ratio tuning constant (≈1.618).
*/
func (seq *Sequencer) Phi() float64 { return seq.phi }

/*
SetEigenMode replaces the internal EigenMode. Nil resets to NewEigenMode().
*/
func (seq *Sequencer) SetEigenMode(eigen *geometry.EigenMode) {
	if eigen == nil {
		seq.eigen = geometry.NewEigenMode()
		return
	}
	seq.eigen = eigen
}

/*
Flush commits the first candidate as a boundary and returns (true, events).
Returns (false, nil) if no candidates. Same event format as Analyze.
*/
func (seq *Sequencer) Flush() (bool, []int) {
	if len(seq.candidates) == 0 {
		return false, nil
	}

	emitK := seq.candidates[0].k
	events := append([]int{}, seq.classifyDirection(seq.buf, emitK))
	events = append(events, geometry.EventPhaseInversion)

	seq.emitSplit(emitK)

	// Shift all remaining candidates and the offset.
	seq.candidates = seq.candidates[1:]
	for i := range seq.candidates {
		seq.candidates[i].k -= emitK
	}
	seq.offset -= emitK

	return true, events
}

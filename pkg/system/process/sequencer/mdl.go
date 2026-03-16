package sequencer

import (
	"math"

	"github.com/theapemachine/six/pkg/numeric/geometry"
	"github.com/theapemachine/six/pkg/store/data"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/process"
)

type Sequencer interface {
	Analyze(pos uint32, byteVal byte) (bool, int, []int, data.Value)
}

/*
Seq discovers natural boundaries in a raw byte stream using the
Minimum Description Length (MDL) principle.
*/
type MDL struct {
	calibrator *process.Calibrator
	eigen      *geometry.EigenMode

	buf  []byte
	dist *process.Distribution

	runningValue data.Value

	prevSegLen  int
	fluxEmitted bool

	lastByteVal  float64
	lastEigenMag float64

	emaPhase float64
	emaPop   float64

	runningMeta data.Value

	tokens     []data.Value
	candidates []candidate
	offset     int

	MinSegmentBytes int

	ShannonCeiling float64

	PhaseThreshold float64
}

type candidate struct {
	k       int
	gain    float64 // penalized MDL gain; used as confidence for emission
	entropy float64 // entropy jump at split (optional extra evidence)
	forced  bool    // Forced boundary: non-negotiable, balancer must not absorb
}

/*
NewMDL creates a MDL with optional calibrator for BIC penalty tuning.
Default MinSegmentBytes=4.
*/
func NewMDL(calibrator *process.Calibrator) *MDL {
	minSeg := int(math.Log2(float64(config.Numeric.NSymbols)) / 2)
	if minSeg < 2 {
		minSeg = 2
	}
	return &MDL{
		calibrator:      calibrator,
		eigen:           geometry.NewEigenMode(),
		dist:            process.NewDistribution(),
		MinSegmentBytes: minSeg,
		ShannonCeiling:  config.Numeric.ShannonCapacity,
		PhaseThreshold:  math.Pi / 2.0,
	}
}

/*
CloneEmpty returns a new Sequencer with the same config (calibrator, eigen, phi)
but empty buffer, distribution, and candidates.
*/
func (seq *MDL) CloneEmpty() *MDL {
	return &MDL{
		calibrator:      seq.calibrator,
		eigen:           seq.eigen,
		dist:            process.NewDistribution(),
		MinSegmentBytes: seq.MinSegmentBytes,
		ShannonCeiling:  seq.ShannonCeiling,
		PhaseThreshold:  seq.PhaseThreshold,
	}
}

/*
Clone returns a deep copy of the Sequencer including buffer, dist, candidates, and offset.
*/
func (seq *MDL) Clone() *MDL {
	c := seq.CloneEmpty()
	c.buf = append([]byte(nil), seq.buf...)
	c.dist = seq.dist.Clone()
	c.prevSegLen = seq.prevSegLen
	c.fluxEmitted = seq.fluxEmitted
	c.lastByteVal = seq.lastByteVal
	c.lastEigenMag = seq.lastEigenMag
	c.emaPhase = seq.emaPhase
	c.emaPop = seq.emaPop
	c.runningMeta = seq.runningMeta
	c.tokens = append([]data.Value(nil), seq.tokens...)
	c.candidates = append([]candidate(nil), seq.candidates...)
	c.offset = seq.offset
	return c
}

/*
Analyze appends byteVal, runs MDL boundary detection, and optionally emits a split.
Returns (true, events) when a boundary is committed; (false, events) otherwise.
events are geometry.Event* constants for PrimeField.Rotate.
*/
func (seq *MDL) Analyze(pos uint32, byteVal byte) (bool, int, []int, data.Value) {
	val, delta, eigenMag := seq.computeSignal(byteVal)

	seq.buf = append(seq.buf, byteVal)
	seq.dist.Add(byteVal)

	seq.runningMeta = seq.runningMeta.RollLeft(1)

	base := data.BaseValue(byteVal)
	relativeOffset := len(seq.buf) - seq.offset - 1
	positioned := base.RollLeft(relativeOffset)

	newBits := 0
	if relativeOffset > 0 {
		hole := data.ValueHole(&positioned, &seq.runningValue)
		newBits = hole.ActiveCount()
	}

	seq.runningValue = seq.runningValue.OR(positioned)

	densityCeiling := seq.ShannonCeiling
	phaseThreshold := seq.PhaseThreshold
	if seq.calibrator != nil {
		densityCeiling = seq.calibrator.DensityCeiling(seq.ShannonCeiling)
		phaseThreshold = seq.calibrator.PhaseLimit(seq.PhaseThreshold)
	}

	canSplit := (len(seq.buf) - seq.offset) >= seq.MinSegmentBytes

	shannonForced := seq.runningValue.ShannonDensity() > densityCeiling && canSplit
	phaseForced := seq.eigen != nil && seq.eigen.Trained && delta > phaseThreshold && canSplit
	primeForced := newBits >= 3 && canSplit // Significant prime spectrum shift

	isBoundary := false
	k := 0
	gain := 0.0

	if shannonForced || phaseForced || primeForced {
		isBoundary = true
		k = len(seq.buf) - seq.offset
		gain = 1.0
	} else if canSplit {
		// MDL acts as a fallback sanity check
		mdlBoundary, mdlK, mdlGain := seq.detectBoundary(seq.buf[seq.offset:], seq.dist)
		if mdlBoundary {
			isBoundary = true
			k = mdlK
			gain = mdlGain
		}
	}

	var events []int

	emitK := 0

	if isBoundary {
		absK := seq.offset + k
		seq.candidates = append(seq.candidates, candidate{k: absK, gain: gain, forced: shannonForced || phaseForced || primeForced})
		seq.offset = absK

		seq.dist = process.NewDistribution()

		for _, b := range seq.buf[seq.offset:] {
			seq.dist.Add(b)
		}

		seq.runningValue = data.Value{}
	}

	if seq.calibrator != nil && seq.eigen != nil && seq.eigen.Trained {
		seq.calibrator.ObservePhase(delta)
	}

	if len(seq.candidates) >= 2 {
		seq.balanceCandidates()
	}

	if len(seq.candidates) >= 2 {
		emitK = seq.candidates[0].k

		events = append(events, seq.classifyDirection(seq.buf, emitK))
		events = append(events, process.EventPhaseInversion)

		for _, ev := range events {
			switch ev {
			case process.EventLowVarianceFlux:
				seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(0))
			case process.EventDensitySpike:
				seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(1))
			case process.EventDensityTrough:
				seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(2))
			case process.EventPhaseInversion:
				seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(3))
			}
		}

		emitMeta := seq.runningMeta

		emitValue := data.Value{}
		for i, b := range seq.buf[:emitK] {
			bc := data.BaseValue(b)
			emitValue = emitValue.OR(bc.RollLeft(i))
		}
		emitDensity := emitValue.ShannonDensity()

		maxBits := float64(emitK * 5)
		actualBits := float64(emitValue.ActiveCount())
		primeCoherence := 0.0
		if maxBits > 0 {
			primeCoherence = math.Max(0, 1.0-(actualBits/maxBits))
		}
		phaseCoherence := math.Exp(-seq.emaPhase)

		seq.emitSplit(emitK)

		seq.candidates = seq.candidates[1:]

		for i := range seq.candidates {
			seq.candidates[i].k -= emitK
		}

		seq.offset -= emitK
		seq.updateEMA(val, delta, eigenMag)

		if seq.calibrator != nil {
			seq.calibrator.FeedbackChunk(emitDensity, primeCoherence, phaseCoherence)
		}

		return true, emitK, events, emitMeta
	}

	if seq.hasFlux() {
		events = append(events, process.EventLowVarianceFlux)
		seq.fluxEmitted = true
		seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(0))
	}

	seq.updateEMA(val, delta, eigenMag)
	return false, emitK, events, seq.runningMeta
}

func (seq *MDL) hasFlux() bool {
	return seq.prevSegLen > 0 && (len(seq.buf)-seq.offset) >= seq.prevSegLen && !seq.fluxEmitted
}

func (seq *MDL) computeSignal(byteVal byte) (val, delta, eigenMag float64) {
	val = float64(byteVal)

	if seq.eigen != nil && seq.eigen.Trained {
		c := data.BaseValue(byteVal)
		theta, phi := seq.eigen.PhaseForValue(&c)
		eigenMag = math.Hypot(theta, phi)
		delta = math.Abs(eigenMag - seq.lastEigenMag)
		return
	}

	delta = math.Abs(val - seq.lastByteVal)
	return
}

func (seq *MDL) detectBoundary(buf []byte, dist *process.Distribution) (bool, int, float64) {
	n := len(buf)
	minSeg := max(seq.MinSegmentBytes, 2)

	if n < 2*minSeg {
		return false, 0, 0
	}

	penaltyScale := 1.0

	if seq.calibrator != nil && seq.calibrator.SensitivityPop() > 0 {
		penaltyScale = seq.calibrator.SensitivityPop()
	}

	bestK := 0
	maxGain := 0.0

	costFull := dist.Cost()
	left := process.NewDistribution()
	right := process.NewDistribution()
	*right = *dist // fast copy of histogram state

	for i := 1; i < n; i++ {
		b := buf[i-1]

		left.Add(b)
		right.Remove(b)

		// Guard: segments must have enough evidence to be statistically significant.
		if i < minSeg || n-i < minSeg {
			continue
		}

		// Calculate parameter counts (subtract 1 because probabilities must sum to 1)
		fullParams := max(dist.NumDistinct()-1, 1)
		leftParams := max(left.NumDistinct()-1, 1)
		rightParams := max(right.NumDistinct()-1, 1)

		// Penalty for the parent model
		penaltyFull := 0.5 * float64(fullParams) * math.Log(float64(n))

		// Penalties for the split sub-models
		penaltyLeft := 0.5 * float64(leftParams) * math.Log(float64(i))
		penaltyRight := 0.5 * float64(rightParams) * math.Log(float64(n-i))

		// Total Gain = (Cost_parent + Penalty_parent) - (Cost_L + Pen_L + Cost_R + Pen_R)
		baseGain := costFull - (left.Cost() + right.Cost())
		penaltyDiff := (penaltyLeft + penaltyRight) - penaltyFull

		penalizedGain := baseGain - (penaltyScale * penaltyDiff)

		if penalizedGain > maxGain {
			maxGain = penalizedGain
			bestK = i
		}
	}

	return maxGain > 0, bestK, maxGain
}

func (seq *MDL) emitSplit(k int) {
	seq.prevSegLen = k
	seq.buf = append([]byte(nil), seq.buf[k:]...) // force copy to avoid leaks
	seq.fluxEmitted = false
}

func (seq *MDL) balanceCandidates() {
	if len(seq.candidates) < 2 {
		return
	}

	// Shannon-forced candidates are non-negotiable.
	if seq.candidates[0].forced || seq.candidates[1].forced {
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

func (seq *MDL) getDistribution(start, end int) *process.Distribution {
	dist := process.NewDistribution()
	for _, b := range seq.buf[start:end] {
		dist.Add(b)
	}
	return dist
}

func (seq *MDL) isSimilar(d1, d2 *process.Distribution) bool {
	if d1.N() == 0 || d2.N() == 0 {
		return false
	}

	costSplit := d1.Cost() + d2.Cost()
	dCombined := d1.Clone()
	dCombined.AddFrom(d2)
	costCombined := dCombined.Cost()

	// Pure dynamic check: if treating them as one distribution is
	// computationally cheaper (via MDL) than splitting, they are similar.
	return costCombined <= costSplit
}

/*
classifyDirection returns EventDensitySpike if right-mean > left-mean, else EventDensityTrough.
Compares buf[:k] vs buf[k:] mean byte values.
*/
func (seq *MDL) classifyDirection(buf []byte, k int) int {
	if k <= 0 || k >= len(buf) {
		return process.EventDensityTrough
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
		return process.EventDensitySpike
	}
	return process.EventDensityTrough
}

func (seq *MDL) updateEMA(val, delta, eigenMag float64) {
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
func (seq *MDL) Forecast(pos int, byteVal byte) (bool, []int, data.Value) {
	buf := append(seq.buf, byteVal)
	dist := *seq.dist
	dist.Add(byteVal)

	window := buf[seq.offset:]
	isBoundary, k, _ := seq.detectBoundary(window, &dist)
	if !isBoundary {
		return false, nil, seq.runningMeta
	}

	events := append([]int{}, seq.classifyDirection(window, k))
	events = append(events, process.EventPhaseInversion)

	tempMeta := seq.runningMeta.RollLeft(1)
	for _, ev := range events {
		switch ev {
		case process.EventLowVarianceFlux:
			tempMeta = tempMeta.XOR(data.BaseValue(0))
		case process.EventDensitySpike:
			tempMeta = tempMeta.XOR(data.BaseValue(1))
		case process.EventDensityTrough:
			tempMeta = tempMeta.XOR(data.BaseValue(2))
		case process.EventPhaseInversion:
			tempMeta = tempMeta.XOR(data.BaseValue(3))
		}
	}

	return true, events, tempMeta
}

/*
FeedbackRetrievalQuality adjusts calibrator.sensitivityPop by 1/phi.
overDiscriminated: increase penalty (fewer splits). underDiscriminated: decrease.
Clamps sensitivityPop to [0.05, 20].
*/
func (seq *MDL) FeedbackRetrievalQuality(overDiscriminated, underDiscriminated bool) {
	if seq.calibrator == nil {
		return
	}

	_, stddev := seq.calibrator.WindowStats()
	adjust := stddev

	if overDiscriminated {
		seq.calibrator.SetSensitivityPop(seq.calibrator.SensitivityPop() * math.Exp(adjust))
	} else if underDiscriminated {
		seq.calibrator.SetSensitivityPop(seq.calibrator.SensitivityPop() * math.Exp(-adjust))
	}
}

/*
Phase returns (emaPhase, costPerByte). costPerByte = dist.Cost()/n.
*/
func (seq *MDL) Phase() (float64, float64) {
	return seq.emaPhase, seq.dist.Cost() / float64(max(seq.dist.N(), 1))
}

/*
SetEigenMode replaces the internal EigenMode. Nil resets to NewEigenMode().
*/
func (seq *MDL) SetEigenMode(eigen *geometry.EigenMode) {
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
func (seq *MDL) Flush() (bool, int, []int, data.Value) {
	if len(seq.candidates) == 0 {
		return false, 0, nil, seq.runningMeta
	}

	emitK := seq.candidates[0].k
	events := append([]int{}, seq.classifyDirection(seq.buf, emitK))
	events = append(events, process.EventPhaseInversion)

	for _, ev := range events {
		switch ev {
		case process.EventLowVarianceFlux:
			seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(0))
		case process.EventDensitySpike:
			seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(1))
		case process.EventDensityTrough:
			seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(2))
		case process.EventPhaseInversion:
			seq.runningMeta = seq.runningMeta.XOR(data.BaseValue(3))
		}
	}

	emitMeta := seq.runningMeta

	seq.emitSplit(emitK)

	// Shift all remaining candidates and the offset.
	seq.candidates = seq.candidates[1:]
	for i := range seq.candidates {
		seq.candidates[i].k -= emitK
	}
	seq.offset -= emitK

	return true, emitK, events, emitMeta
}

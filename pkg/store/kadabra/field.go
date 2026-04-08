package kadabra

import (
	"errors"
	"maps"
	"math"
	"slices"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/numeric/gf"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
minAlignmentRatio floors alignmentRatio before reciprocals so misaligned
learn/decay multipliers cannot diverge when dominant energy is tiny.
*/
const minAlignmentRatio = 1e-3

/*
digestMap is an immutable snapshot of remote trie digests for eigenmode work.
*/
type digestMap struct {
	m map[uint64]Digest
}

/*
modesProjection stores the latest DetectModes result without a mutex so
readers can query mode membership lock-free.
*/
type modesProjection struct {
	modes    []geometry.Eigenmode
	dominant int
}

/*
Field lives in the context of a Kadabra Node, and forms a mesh
across all the Markov Tries that the node acts as a routing hub for.

All thresholds and multipliers are Derived chains — no magic numbers.
The coupling threshold tracks the mean observed coupling and gates
above it. Decay and learn pressure clamps each track their own spread.
Alignment multipliers emerge from the ratio of dominant mode energy
to total energy.
*/
type Field struct {
	digests     atomic.Pointer[digestMap]
	projection  atomic.Pointer[modesProjection]
	nodePhase   atomic.Pointer[gf.Vector8191]
	globalPhase atomic.Pointer[gf.Vector65537]
	owner       *Node
	phase       *geometry.Phase

	couplingThreshold  *numeric.Derived
	pressureDecayClamp *numeric.Derived
	pressureLearnClamp *numeric.Derived
	alignmentRatio     *numeric.Derived
	dominantPhaseIndex atomic.Uint32
	dominantPhaseGain  atomic.Uint64
}

/*
NewField constructs a new Field for the given node.
*/
func NewField(owner *Node) *Field {
	field := &Field{
		owner: owner,
		phase: geometry.NewPhase(),

		couplingThreshold: numeric.NewDerived(
			numeric.WithDynamics(adaptive.NewEMA()),
		),

		pressureDecayClamp: numeric.NewDerived(
			numeric.WithDynamics(adaptive.NewSpread()),
		),

		pressureLearnClamp: numeric.NewDerived(
			numeric.WithDynamics(adaptive.NewSpread()),
		),

		alignmentRatio: numeric.NewDerived(
			numeric.WithDynamics(
				adaptive.NewRatio(0),
				adaptive.NewSigmaClamp(3, 8, 0.0625),
			),
		),
	}

	field.nodePhase.Store(gf.NewVector8191())
	field.globalPhase.Store(gf.NewVector65537())
	field.dominantPhaseIndex.Store(0)

	return field
}

/*
digestLookup returns one digest origin if present.
*/
func (field *Field) digestLookup(origin uint64) (Digest, bool) {
	if field == nil {
		return Digest{}, false
	}

	dm := field.digests.Load()

	if dm == nil || dm.m == nil {
		return Digest{}, false
	}

	d, ok := dm.m[origin]

	return d, ok
}

/*
ModeCount returns how many eigenmodes were detected in the last projection.
*/
func (field *Field) ModeCount() int {
	if field == nil {
		return 0
	}

	proj := field.projection.Load()

	if proj == nil {
		return 0
	}

	return len(proj.modes)
}

/*
ModeMembers returns a copy of member node IDs for the mode at modeIdx,
or nil if out of range.
*/
func (field *Field) ModeMembers(modeIdx int) []uint64 {
	if field == nil || modeIdx < 0 {
		return nil
	}

	proj := field.projection.Load()

	if proj == nil || modeIdx >= len(proj.modes) {
		return nil
	}

	members := proj.modes[modeIdx].Members()
	out := make([]uint64, len(members))
	copy(out, members)

	return out
}

/*
ModeEnergy returns the aggregate energy score for modeIdx, or 0 if
out of range.
*/
func (field *Field) ModeEnergy(modeIdx int) float64 {
	if field == nil || modeIdx < 0 {
		return 0
	}

	proj := field.projection.Load()

	if proj == nil || modeIdx >= len(proj.modes) {
		return 0
	}

	return proj.modes[modeIdx].Energy()
}

/*
DominantModeIndex returns the index of the highest-energy mode, or -1
if none.
*/
func (field *Field) DominantModeIndex() int {
	if field == nil {
		return -1
	}

	proj := field.projection.Load()

	if proj == nil {
		return -1
	}

	return proj.dominant
}

/*
DominantModeEnergy returns the energy of the dominant mode, or 0 if
there is no dominant mode.
*/
func (field *Field) DominantModeEnergy() float64 {
	if field == nil {
		return 0
	}

	proj := field.projection.Load()

	if proj == nil || proj.dominant < 0 || proj.dominant >= len(proj.modes) {
		return 0
	}

	return proj.modes[proj.dominant].Energy()
}

/*
NodePhase returns the latest node-scale GF(8191) projection.
*/
func (field *Field) NodePhase() gf.Vector8191 {
	if field == nil {
		return gf.Vector8191{}
	}

	nodePhase := field.nodePhase.Load()

	if nodePhase == nil {
		return gf.Vector8191{}
	}

	return *nodePhase
}

/*
GlobalPhase returns the latest mesh-scale GF(65537) projection.
*/
func (field *Field) GlobalPhase() gf.Vector65537 {
	if field == nil {
		return gf.Vector65537{}
	}

	globalPhase := field.globalPhase.Load()

	if globalPhase == nil {
		return gf.Vector65537{}
	}

	return *globalPhase
}

/*
DominantPhaseIndex returns the strongest global phase lane, or -1 if
the field has not settled into a phase.

The underlying atomic stores lane index plus one: 0 means “unset”, 1 means
lane 0, 2 means lane 1, etc. (see setDominantPhase). Examples: stored 0 -> -1;
stored 1 -> 0.
*/
func (field *Field) DominantPhaseIndex() int {
	if field == nil {
		return -1
	}

	encoded := field.dominantPhaseIndex.Load()

	if encoded == 0 {
		return -1
	}

	return int(encoded) - 1
}

/*
DominantPhaseStrength returns the concentration of the strongest
global phase lane.
*/
func (field *Field) DominantPhaseStrength() float64 {
	if field == nil {
		return 0
	}

	return math.Float64frombits(field.dominantPhaseGain.Load())
}

/*
Absorb integrates a received digest, recomputes eigenmodes,
and projects field pressure onto the owning node's tries.
*/
func (field *Field) Absorb(digest Digest) {
	if field == nil {
		return
	}

	if !field.absorbDigest(digest) {
		return
	}

	viz.DefaultBus.Publish(viz.GossipReceived(
		field.owner.ID, digest.Origin, digest.Epoch,
	))

	_, _ = field.Project()
}

func (field *Field) absorbDigest(digest Digest) bool {
	for {
		old := field.digests.Load()
		var base map[uint64]Digest

		if old != nil && old.m != nil {
			if existing, ok := old.m[digest.Origin]; ok && digest.Epoch <= existing.Epoch {
				return false
			}

			base = maps.Clone(old.m)
		} else {
			base = make(map[uint64]Digest)
		}

		base[digest.Origin] = digest
		newSnap := &digestMap{m: base}

		if field.digests.CompareAndSwap(old, newSnap) {
			return true
		}
	}
}

/*
Project recomputes eigenmodes and applies asymmetric pressure to the
owning node's tries based on whether each belongs to the dominant mode.

Aligned tries (in the dominant eigenmode, in-phase):
  - Decay pressure suppressed — knowledge retained longer
  - Learning pressure amplified — absorbs related input faster

Misaligned tries (outside the dominant mode, or anti-phase):
  - Decay pressure amplified — stale knowledge pruned faster
  - Learning pressure suppressed — doesn't waste effort on noise

This asymmetric fork IS the attention mechanism. No matrices, no softmax.
The field selects by differential survival.

All multipliers are derived:
  - Coupling threshold: smoothed mean of observed coupling values
  - Pressure clamps: separate smoothed spread for decay and learn streams
  - Alignment ratio: dominant mode energy / total energy, smoothed
*/
func (field *Field) Project(values ...Routable) (*algo.Prediction, error) {
	_ = values

	if field.owner == nil {
		return nil, nil
	}

	localNodePhase := field.refreshNodePhase()
	digestSnap := field.localDigestSnapshot(localNodePhase)
	dm := field.digests.Load()

	if dm != nil && dm.m != nil {
		for origin, digest := range dm.m {
			digestSnap[origin] = digest
		}
	}

	if len(digestSnap) < 2 {
		return nil, nil
	}

	globalPhase := field.refreshGlobalPhase(localNodePhase, digestSnap)
	globalMode := geometry.DetectPhaseMode65537(*globalPhase)
	projection := algo.NewPrediction()

	if globalMode.Index >= 0 {
		projection.Signals[algo.GlobalPhase] = numeric.NewDerivedFrom(float64(globalMode.Index))
		projection.Signals[algo.PhaseConcentration] = numeric.NewDerivedFrom(globalMode.Concentration)
	}

	participants := make([]geometry.ModeParticipant, 0, len(digestSnap))
	var totalEnergy float64

	affinityByOrigin := make(
		map[uint64]*primitive.Affinity,
		len(digestSnap),
	)

	for origin, digest := range digestSnap {
		affinityByOrigin[origin] = primitive.NewAffinityFromVector(digest.Affinity)

		participants = append(participants, geometry.ModeParticipant{
			Origin: origin,
			Energy: digest.SurprisalMean,
		})

		totalEnergy += digest.SurprisalMean
	}

	couplingFn := func(a, b uint64) float64 {
		affA := affinityByOrigin[a]
		affB := affinityByOrigin[b]

		return affA.Coupling(affB)
	}

	// Feed all pairwise coupling values into the threshold chain
	// so it learns the distribution and can gate meaningfully.
	for idx := range participants {
		for jdx := idx + 1; jdx < len(participants); jdx++ {
			coupling := couplingFn(
				participants[idx].Origin,
				participants[jdx].Origin,
			)

			field.couplingThreshold.Next(coupling)

			viz.DefaultBus.Publish(viz.TrieCouplingEvent(
				field.owner.ID,
				idx, jdx,
				coupling,
			))
		}
	}

	threshold := field.couplingThreshold.Value()

	modes, dominant := geometry.DetectModes(
		participants,
		threshold,
		couplingFn,
	)

	field.projection.Store(&modesProjection{modes: modes, dominant: dominant})

	if dominant >= 0 && dominant < len(modes) {
		viz.DefaultBus.Publish(viz.EigenmodeDetected(
			field.owner.ID,
			len(modes),
			modes[dominant].Energy(),
		))
	}

	// Derive alignment ratio from mode energy distribution.
	if dominant >= 0 && dominant < len(modes) && totalEnergy > 0 {
		dominantEnergy := modes[dominant].Energy()
		field.alignmentRatio.Next(dominantEnergy, totalEnergy)
	}

	tries := field.owner.triesSnapshot()

	var pressureErr error

	for trieIdx := range tries {
		cluster := tries[trieIdx]

		if cluster == nil {
			continue
		}

		trieID := (field.owner.ID << 32) | uint64(uint32(trieIdx+1))

		local, hasLocal := digestSnap[trieID]

		if !hasLocal {
			continue
		}

		// Per-trie signal snapshot.
		viz.DefaultBus.Publish(viz.TrieSignalEvent(
			field.owner.ID, trieIdx,
			local.SurprisalMean, local.ClassEntropy, local.GrowthRate,
		))

		aligned := false
		modeIdx := -1

		if dominant >= 0 && dominant < len(modes) {
			if slices.Contains(modes[dominant].Members(), trieID) {
				aligned = true
				modeIdx = dominant
			}
		}

		energy := 0.0

		if modeIdx >= 0 && modeIdx < len(modes) {
			energy = modes[modeIdx].Energy()
		}

		viz.DefaultBus.Publish(viz.TrieModeEvent(
			field.owner.ID, trieIdx, modeIdx, aligned, energy,
		))

		var (
			weightedSurprisal float64
			weightedGrowth    float64
			totalWeight       float64
		)

		for origin, digest := range digestSnap {
			if origin == trieID {
				continue
			}

			phaseCoupling := field.phase.Coupling(local.SurprisalGrowth, digest.SurprisalGrowth)
			weight := primitive.NewAffinityFromVector(
				local.Affinity,
			).Coupling(
				primitive.NewAffinityFromVector(digest.Affinity),
			) * (1.0 + phaseCoupling)

			if weight <= 0 {
				continue
			}

			totalWeight += weight
			weightedSurprisal += weight * digest.SurprisalMean
			weightedGrowth += weight * digest.GrowthRate
		}

		if totalWeight == 0 {
			continue
		}

		fieldSurprisal := weightedSurprisal / totalWeight
		fieldGrowth := weightedGrowth / totalWeight

		rawDecay := fieldSurprisal - local.SurprisalMean
		rawLearn := fieldGrowth - local.GrowthRate

		field.pressureDecayClamp.Next(rawDecay)
		field.pressureLearnClamp.Next(rawLearn)

		clampLimitDecay := field.pressureDecayClamp.Value()
		clampLimitLearn := field.pressureLearnClamp.Value()

		if clampLimitDecay <= 0 {
			clampLimitDecay = 1
		}

		if clampLimitLearn <= 0 {
			clampLimitLearn = 1
		}

		// Alignment ratio acts as the asymmetric multiplier.
		// Aligned tries get the ratio as learn boost and its
		// inverse as decay suppression. Misaligned get the
		// opposite. The ratio itself is derived from mode
		// energy distribution — no config constants needed.
		ratio := field.alignmentRatio.Value()
		safeRatio := math.Max(ratio, minAlignmentRatio)
		inv := 1.0 / safeRatio

		var decayMul, learnMul float64

		if aligned {
			decayMul = inv
			learnMul = safeRatio
		} else {
			decayMul = safeRatio
			learnMul = inv
		}

		localMode := geometry.DetectPhaseMode257(cluster.LocalPhase())
		phaseAlignment := geometry.PhaseAlignment(localMode, globalMode)
		phaseGain := math.Max(globalMode.Concentration, minAlignmentRatio)

		if globalMode.Index >= 0 {
			constructiveBoost := 1 + (phaseGain * phaseAlignment)
			destructiveBoost := 1 + (phaseGain * math.Max(1-phaseAlignment, minAlignmentRatio))

			if aligned {
				learnMul *= constructiveBoost
				decayMul /= constructiveBoost
			} else {
				decayMul *= destructiveBoost
				learnMul *= math.Max(phaseAlignment, minAlignmentRatio)
			}
		}

		decay := clamp(rawDecay*decayMul, clampLimitDecay)
		learn := clamp(rawLearn*learnMul, clampLimitLearn)

		if err := cluster.ApplyFieldPressure(
			decay,
			learn,
			decay,
			float64(globalMode.Index),
			globalMode.Concentration,
		); err != nil {
			pressureErr = errors.Join(pressureErr, err)
		}

		viz.DefaultBus.Publish(viz.TriePressureEvent(
			field.owner.ID, trieIdx, decay, learn, decayMul, learnMul,
		))

		viz.DefaultBus.Publish(viz.FieldPressureEvent(
			field.owner.ID, decay, learn, decay,
		))
	}

	return projection, pressureErr
}

func clamp(val, limit float64) float64 {
	if val > limit {
		return limit
	}

	if val < -limit {
		return -limit
	}

	return val
}

func (field *Field) refreshNodePhase() *gf.Vector8191 {
	nodePhase := gf.NewVector8191()

	if field == nil || field.owner == nil {
		return nodePhase
	}

	tries := field.owner.triesSnapshot()
	trieLimit := len(tries)

	if core.Cfg.Kadabra.MaxMeshPeers > 0 {
		trieLimit = min(trieLimit, core.Cfg.Kadabra.MaxMeshPeers)
	}

	for trieIdx := range trieLimit {
		cluster := tries[trieIdx]

		if cluster == nil {
			continue
		}

		localPhase := cluster.LocalPhase()
		nodePhase.AccumulateProjected257(&localPhase, trieIdx)
	}

	field.nodePhase.Store(nodePhase)

	return nodePhase
}

func (field *Field) refreshGlobalPhase(
	localNodePhase *gf.Vector8191,
	digestSnap map[uint64]Digest,
) *gf.Vector65537 {
	globalPhase := gf.NewVector65537()
	slotIndex := 0

	if localNodePhase != nil {
		globalPhase.AccumulateProjected8191(localNodePhase, slotIndex)
		slotIndex++
	}

	digestOrigins := make([]uint64, 0, len(digestSnap))

	for origin := range digestSnap {
		digestOrigins = append(digestOrigins, origin)
	}

	slices.Sort(digestOrigins)

	for _, origin := range digestOrigins {
		digest := digestSnap[origin]
		globalPhase.AccumulateProjected8191(&digest.NodePhase, slotIndex)
		slotIndex++
	}

	field.globalPhase.Store(globalPhase)
	field.setDominantPhase(geometry.DetectPhaseMode65537(*globalPhase))

	return globalPhase
}

func (field *Field) setDominantPhase(globalMode geometry.PhaseMode) {
	if globalMode.Index < 0 {
		field.dominantPhaseIndex.Store(0)
		field.dominantPhaseGain.Store(0)

		return
	}

	// Index is stored as lane+1 so DominantPhaseIndex can use 0 as “none”.
	field.dominantPhaseIndex.Store(uint32(globalMode.Index + 1))
	field.dominantPhaseGain.Store(math.Float64bits(globalMode.Concentration))
}

func (field *Field) localDigestSnapshot(localNodePhase *gf.Vector8191) map[uint64]Digest {
	digestSnap := make(map[uint64]Digest)

	if field == nil || field.owner == nil {
		return digestSnap
	}

	tries := field.owner.triesSnapshot()
	epoch := atomic.LoadUint64(&field.owner.epoch)

	for trieIdx := range tries {
		cluster := tries[trieIdx]

		if cluster == nil {
			continue
		}

		origin := (field.owner.ID << 32) | uint64(uint32(trieIdx+1))
		surprisalMean := cluster.Signal(algo.Surprisal)
		classEntropy := cluster.Signal(algo.Entropy)
		growthRate := cluster.Signal(algo.GrowthRate)

		var prevSurprisal float64

		if previous, ok := field.digestLookup(origin); ok {
			prevSurprisal = previous.SurprisalMean
		}

		digestSnap[origin] = Digest{
			Origin:          origin,
			Affinity:        cluster.Affinity.Vector(),
			NodePhase:       *localNodePhase,
			SurprisalMean:   surprisalMean,
			SurprisalGrowth: surprisalMean - prevSurprisal,
			SurprisalPrev:   prevSurprisal,
			ClassEntropy:    classEntropy,
			GrowthRate:      growthRate,
			Epoch:           epoch,
		}
	}

	return digestSnap
}

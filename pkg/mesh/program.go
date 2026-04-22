package mesh

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

type banditArm struct {
	Pulls uint64
	Value float64
}

/*
Program is a contextual bandit over firmware. It keeps the policy inside the
substrate boundary: FieldMetrics are the context, firmware is the action, and
the next metric refresh supplies reward through local changes in pressure,
crystallisation, and dominant eigenmode concentration.
*/
type Program struct {
	mu         sync.Mutex
	arms       map[core.FirmwareType]*banditArm
	rng        *rand.Rand
	totalPulls uint64
	lastAction core.FirmwareType
	lastMetric FieldMetrics
	hasLast    bool
}

func NewProgram() *Program {
	arms := make(map[core.FirmwareType]*banditArm, len(core.Cfg.Programs))

	for fw := range core.Cfg.Programs {
		arms[fw] = &banditArm{Value: 0.1}
	}

	return &Program{
		arms: arms,
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (program *Program) Select() core.FirmwareType {
	return program.SelectFor(nil)
}

func (program *Program) SelectFor(metrics *FieldMetrics) core.FirmwareType {
	if program == nil {
		return core.BEAM
	}

	program.mu.Lock()
	defer program.mu.Unlock()

	if len(program.arms) == 0 {
		return core.BEAM
	}

	var selected core.FirmwareType
	bestScore := math.Inf(-1)
	total := float64(program.totalPulls + 1)

	for fw, arm := range program.arms {
		prior := firmwarePrior(fw, metrics)
		explore := math.Sqrt(math.Log(total+1) / float64(arm.Pulls+1))
		jitter := program.rng.Float64() * 1e-6
		score := arm.Value + prior + 0.25*explore + jitter

		if selected == "" || score > bestScore {
			selected = fw
			bestScore = score
		}
	}

	if selected == "" {
		selected = core.UNSUPERVISED_LEARN
	}

	arm := program.arms[selected]
	arm.Pulls++
	program.totalPulls++
	program.lastAction = selected

	if metrics != nil {
		program.lastMetric = *metrics
		program.hasLast = true
	}

	errnie.Trace("mesh.Program.SelectFor", "selectedFw", string(selected))
	return selected
}

func (program *Program) Update(metrics *FieldMetrics) {
	if program == nil || metrics == nil {
		return
	}

	program.mu.Lock()
	defer program.mu.Unlock()

	if program.hasLast && program.lastAction != "" {
		if arm := program.arms[program.lastAction]; arm != nil {
			reward := rewardDelta(program.lastMetric, *metrics)
			n := float64(max(arm.Pulls, uint64(1)))
			arm.Value += (reward - arm.Value) / n
			if arm.Value < -1 {
				arm.Value = -1
			}
			if arm.Value > 1 {
				arm.Value = 1
			}
		}
	}

	program.lastMetric = *metrics
	program.hasLast = true
}

func rewardDelta(prev, next FieldMetrics) float64 {
	crystalGain := next.Crystallization - prev.Crystallization
	modeGain := next.DominantRatio - prev.DominantRatio
	pressureDrop := prev.PressureMult - next.PressureMult
	coverageGain := next.Coverage - prev.Coverage

	reward := 0.45*crystalGain + 0.25*modeGain + 0.20*pressureDrop + 0.10*coverageGain

	if next.MemberCount == 0 {
		reward -= 0.05
	}

	if math.IsNaN(reward) || math.IsInf(reward, 0) {
		return 0
	}

	return reward
}

func firmwarePrior(fw core.FirmwareType, metrics *FieldMetrics) float64 {
	if metrics == nil {
		switch fw {
		case core.AFFINITY, core.LINK, core.UNSUPERVISED_LEARN:
			return 0.15
		default:
			return 0.02
		}
	}

	switch fw {
	case core.UNSUPERVISED_LEARN:
		return 0.30 * (1 - clamp01(metrics.Coverage))
	case core.CLASSIFY_READOUT, core.LINK:
		return 0.25 * clamp01(metrics.Crystallization)
	case core.HYPOTHESIS, core.FALSIFICATION:
		return 0.20 * clamp01(metrics.DominantRatio)
	case core.INTERVENTION, core.CAUSAL_EXPLORE, core.CAUSAL_HUB:
		return 0.18 * clamp01(metrics.PressureMult)
	case core.AFFINITY:
		if metrics.MemberCount < 4 {
			return 0.20
		}
		return 0.04
	default:
		return 0.12 * clamp01(metrics.PressureMult)
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

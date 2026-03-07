package generation

import (
	"sync"
	"sync/atomic"
)

type BranchPolicy struct {
	Enabled         bool
	MarginThreshold float64
	MaxRetained     int
}

type ObservabilitySnapshot struct {
	LowMarginEvents  uint64
	RetainedBranches uint64
	AnchorVetoEvents uint64
}

type PolicyTracker struct {
	mu               sync.RWMutex
	policy           BranchPolicy
	lowMarginEvents  atomic.Uint64
	retainedBranches atomic.Uint64
	anchorVetoEvents atomic.Uint64
}

func DefaultBranchPolicy() BranchPolicy {
	return BranchPolicy{Enabled: false, MarginThreshold: 0.05, MaxRetained: 2}
}

func normalizeBranchPolicy(policy BranchPolicy) BranchPolicy {
	if policy.MaxRetained <= 0 {
		policy.MaxRetained = 1
	}
	if policy.MarginThreshold < 0 {
		policy.MarginThreshold = 0
	}
	if policy.MarginThreshold > 1 {
		policy.MarginThreshold = 1
	}

	return policy
}

func NewPolicyTracker(policy BranchPolicy) *PolicyTracker {
	return &PolicyTracker{policy: normalizeBranchPolicy(policy)}
}

func (tracker *PolicyTracker) SetPolicy(policy BranchPolicy) {
	tracker.mu.Lock()
	tracker.policy = normalizeBranchPolicy(policy)
	tracker.mu.Unlock()
}

func (tracker *PolicyTracker) Policy() BranchPolicy {
	tracker.mu.RLock()
	policy := tracker.policy
	tracker.mu.RUnlock()

	return policy
}

func (tracker *PolicyTracker) TrackMargin(margin float64, retained *int) {
	policy := tracker.Policy()
	if !policy.Enabled {
		return
	}

	if margin >= policy.MarginThreshold {
		return
	}

	tracker.lowMarginEvents.Add(1)
	if retained != nil && *retained < policy.MaxRetained {
		tracker.retainedBranches.Add(1)
		(*retained)++
	}
}

func (tracker *PolicyTracker) TrackAnchorVeto() {
	tracker.anchorVetoEvents.Add(1)
}

func (tracker *PolicyTracker) Snapshot() ObservabilitySnapshot {
	return ObservabilitySnapshot{
		LowMarginEvents:  tracker.lowMarginEvents.Load(),
		RetainedBranches: tracker.retainedBranches.Load(),
		AnchorVetoEvents: tracker.anchorVetoEvents.Load(),
	}
}

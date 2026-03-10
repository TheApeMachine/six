package store

import (
	"sync"

	"github.com/theapemachine/six/geometry"
)

type ManifoldSubscriber interface {
	ApplyPermute3Cycle(a, b, c int)
	ApplyPermuteDoubleTransposition(a, b, c, d int)
	ApplyPermute5Cycle(a, b, c, d, e int)
	ApplySetRotState(next uint8)
	ApplyIncrementWinding()
	ApplyComposeRotation(r geometry.GFRotation)
	ApplyState(state uint8)
}

type mathematicalStateMachine struct {
	mu       sync.RWMutex
	rotState uint8
	state    uint8
	subs     []ManifoldSubscriber
}

func newMathematicalStateMachine() *mathematicalStateMachine {
	return &mathematicalStateMachine{}
}

func (sm *mathematicalStateMachine) Subscribe(sub ManifoldSubscriber) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, s := range sm.subs {
		if s == sub {
			return
		}
	}
	sm.subs = append(sm.subs, sub)
}

func (sm *mathematicalStateMachine) RotState() uint8 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.rotState
}

func (sm *mathematicalStateMachine) State() uint8 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *mathematicalStateMachine) ApplyEvent(event int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if event < 0 || event >= len(geometry.StateTransitionMatrix[0]) {
		return
	}

	if int(sm.rotState) < len(geometry.StateTransitionMatrix) {
		next := geometry.StateTransitionMatrix[sm.rotState][event]
		if next != 255 {
			sm.rotState = next
			for _, sub := range sm.subs {
				sub.ApplySetRotState(next)
			}
		}
	}

	if sm.state == 1 {
		for _, sub := range sm.subs {
			sub.ApplyIncrementWinding()
		}
	}

	rot := geometry.EventRotation(event)
	for _, sub := range sm.subs {
		sub.ApplyComposeRotation(rot)
	}

	switch event {
	case geometry.EventDensitySpike:
		for _, sub := range sm.subs {
			sub.ApplyPermute3Cycle(0, 1, 2)
		}
	case geometry.EventPhaseInversion:
		for _, sub := range sm.subs {
			sub.ApplyPermuteDoubleTransposition(0, 3, 1, 4)
		}
	case geometry.EventDensityTrough:
		for _, sub := range sm.subs {
			sub.ApplyPermute3Cycle(0, 2, 1)
		}
	case geometry.EventLowVarianceFlux:
		for _, sub := range sm.subs {
			sub.ApplyPermute5Cycle(0, 1, 2, 3, 4)
		}
	}
}

func (sm *mathematicalStateMachine) mitosis() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.state == 1 {
		return
	}
	sm.state = 1
	for _, sub := range sm.subs {
		sub.ApplyState(1)
	}
}

func (sm *mathematicalStateMachine) deMitosis() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.state == 0 {
		return
	}
	sm.state = 0
	sm.rotState = sm.rotState % 24
	for _, sub := range sm.subs {
		sub.ApplyState(0)
		sub.ApplySetRotState(sm.rotState)
	}
}

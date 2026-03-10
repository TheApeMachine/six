package store

import (
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

type MathematicalStateMachine struct {
	rotState uint8
	state    uint8
	subs     []ManifoldSubscriber
}

func NewMathematicalStateMachine() *MathematicalStateMachine {
	return &MathematicalStateMachine{}
}

func (sm *MathematicalStateMachine) Subscribe(sub ManifoldSubscriber) {
	sm.subs = append(sm.subs, sub)
}

func (sm *MathematicalStateMachine) RotState() uint8 {
	return sm.rotState
}

func (sm *MathematicalStateMachine) State() uint8 {
	return sm.state
}

func (sm *MathematicalStateMachine) ApplyEvent(event int) {
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

func (sm *MathematicalStateMachine) Mitosis() {
	if sm.state == 1 {
		return
	}
	sm.state = 1
	for _, sub := range sm.subs {
		sub.ApplyState(1)
	}
}

func (sm *MathematicalStateMachine) DeMitosis() {
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

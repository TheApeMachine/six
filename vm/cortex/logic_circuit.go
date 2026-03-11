package cortex

import (
	"math"

	"github.com/theapemachine/six/data"
)

/*
LogicCircuit is an ordered reusable tool circuit exported from cortex.
Each step is a rule whose payload can drive the next step's interface.
*/
type LogicCircuit struct {
	Steps   []LogicRule
	Program data.Chord
	Support int
}

/*
Len returns the number of sequential logical steps in the circuit.
*/
func (circuit LogicCircuit) Len() int {
	return len(circuit.Steps)
}

/*
Entry returns the triggering interface chord for the circuit.
*/
func (circuit LogicCircuit) Entry() data.Chord {
	if len(circuit.Steps) == 0 {
		return data.Chord{}
	}

	return circuit.Steps[0].Interface
}

/*
Exit returns the final emitted payload chord for the circuit.
*/
func (circuit LogicCircuit) Exit() data.Chord {
	if len(circuit.Steps) == 0 {
		return data.Chord{}
	}

	last := circuit.Steps[len(circuit.Steps)-1]
	if last.Payload.ActiveCount() > 0 {
		return last.Payload
	}

	return last.Interface
}

/*
Payloads returns the emitted payload sequence for the circuit.
*/
func (circuit LogicCircuit) Payloads() []data.Chord {
	if len(circuit.Steps) == 0 {
		return nil
	}

	payloads := make([]data.Chord, 0, len(circuit.Steps))
	for _, step := range circuit.Steps {
		payload := step.Payload
		if payload.ActiveCount() == 0 {
			payload = step.Interface
		}
		if payload.ActiveCount() == 0 {
			continue
		}
		payloads = append(payloads, payload)
	}

	return payloads
}

/*
Weight returns a smooth score for the circuit's evidence mass and span.
*/
func (circuit LogicCircuit) Weight() float64 {
	if circuit.Support <= 0 || len(circuit.Steps) == 0 {
		return 0
	}

	spanScale := 1.0 + float64(len(circuit.Steps)-1)*0.35
	return math.Log1p(float64(circuit.Support)) * spanScale
}

/*
Signature returns a stable byte signature for deduplicating circuits.
*/
func (circuit LogicCircuit) Signature() string {
	if len(circuit.Steps) == 0 {
		return ""
	}

	buf := make([]byte, 0, len(circuit.Steps)*3*64+64)
	for _, step := range circuit.Steps {
		buf = append(buf, step.Interface.Bytes()...)
		buf = append(buf, step.Payload.Bytes()...)
		buf = append(buf, step.Program.Bytes()...)
	}
	buf = append(buf, circuit.Program.Bytes()...)
	return string(buf)
}

func copyLogicRules(rules []LogicRule) []LogicRule {
	if len(rules) == 0 {
		return nil
	}

	copied := make([]LogicRule, len(rules))
	copy(copied, rules)
	return copied
}

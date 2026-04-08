package algo

import (
	"errors"
	"log"
)

/*
Stack is the shared orchestration object for algorithm slices. It pushes
one Prediction envelope through every Algorithm, then exposes a merged
view of their outputs without the caller needing to know concrete types.
*/
type Stack struct {
	algorithms []Algorithm
}

/*
NewStack constructs a Stack from the given algorithms, preserving order.
Order matters because Update executes sequentially and later signal merges
overwrite earlier keys when they collide.
*/
func NewStack(algorithms ...Algorithm) *Stack {
	stack := &Stack{}

	if len(algorithms) == 0 {
		return stack
	}

	stack.algorithms = append(stack.algorithms, algorithms...)

	return stack
}

/*
Update broadcasts prediction through every Algorithm in order and returns
the merged stack Value after the update completes. Failures are joined
and logged, but one algorithm failing does not stop the rest of the stack.
*/
func (stack *Stack) Update(prediction *Prediction) (*Prediction, error) {
	if stack == nil {
		return NewPrediction(), nil
	}

	var joined error

	for _, algorithm := range stack.algorithms {
		if algorithm == nil {
			continue
		}

		_, err := algorithm.Update(prediction)

		if err != nil {
			log.Printf(
				"algo.Stack.Update: algorithm %T failed: %v",
				algorithm,
				err,
			)

			joined = errors.Join(joined, err)
		}
	}

	return stack.Value(), joined
}

/*
Value merges the current Value of every Algorithm into one Prediction.
This is the universal upward projection for callers like tries and nodes.
*/
func (stack *Stack) Value() *Prediction {
	if stack == nil {
		return NewPrediction()
	}

	merged := NewPrediction()

	for _, algorithm := range stack.algorithms {
		if algorithm == nil {
			continue
		}

		merged.Merge(algorithm.Value())
	}

	return merged
}

/*
Signals returns the stack's current signal values as a flat map.
Duplicate keys are last-writer-wins in stack order.
*/
func (stack *Stack) Signals() map[SignalType]float64 {
	out := make(map[SignalType]float64)

	if stack == nil {
		return out
	}

	for _, algorithm := range stack.algorithms {
		if algorithm == nil {
			continue
		}

		prediction := algorithm.Value()

		if prediction == nil || prediction.Signals == nil {
			continue
		}

		for signalType, signal := range prediction.Signals {
			if signal == nil {
				continue
			}

			out[signalType] = signal.Value()
		}
	}

	return out
}

/*
Signal returns one current signal value from the first Algorithm in the
stack that exposes it, matching the previous store-level ownership lookup.
*/
func (stack *Stack) Signal(signalType SignalType) float64 {
	if stack == nil {
		return 0
	}

	for _, algorithm := range stack.algorithms {
		if algorithm == nil {
			continue
		}

		prediction := algorithm.Value()

		if prediction == nil || prediction.Signals == nil {
			continue
		}

		if signal, ok := prediction.Signals[signalType]; ok && signal != nil {
			return signal.Value()
		}
	}

	return 0
}

/*
Algorithms returns a shallow copy of the stack order for callers that
need to inspect the configured slice without mutating it.
*/
func (stack *Stack) Algorithms() []Algorithm {
	if stack == nil {
		return nil
	}

	return append([]Algorithm(nil), stack.algorithms...)
}

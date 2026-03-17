package errnie

import (
	"fmt"
	"sync/atomic"
)

/*
State is an embeddable error state tracker for objects. Once an object
records an error via Handle, all subsequent Guard/GuardVoid calls on
that state skip execution — avoiding the panic/recover overhead on a
broken instance.

When a recovery function is provided via StateWithRecovery, Handle
spawns it in a goroutine on the first error. The recovery function
should attempt to restore the object and call Heal() on success,
which clears the error state and allows Guard/GuardVoid to resume.

	state := errnie.NewState("spatial-index",
		errnie.StateWithRecovery(func(s *errnie.State) {
			breaker.RecordFailure()
			forest.Close()
			newForest, err := dmt.NewForest(config)
			if err != nil {
				return // stay broken, circuit stays open
			}
			s.Heal()
			breaker.RecordSuccess()
		}),
	)
*/
type State struct {
	err      error
	context  string
	recovery func(*State)
	healing  atomic.Bool
}

/*
stateOpts configures State with options.
*/
type stateOpts func(*State)

/*
NewState creates a fresh State with the given context label.
The context is prefixed to every error message for structured logging.
*/
func NewState(context string, opts ...stateOpts) *State {
	state := &State{context: context}

	for _, opt := range opts {
		opt(state)
	}

	return state
}

/*
Handle records the first error and logs it with the state's context.
Subsequent errors are logged but do not overwrite the original cause.
If a recovery function is configured, it is spawned exactly once.
*/
func (state *State) Handle(err error) {
	wrapped := fmt.Errorf("%s: %w", state.context, err)
	Error(wrapped)

	if state.err == nil {
		state.err = wrapped

		if state.recovery != nil && state.healing.CompareAndSwap(false, true) {
			go state.recovery(state)
		}
	}
}

/*
Failed reports whether the state has recorded an error.
*/
func (state *State) Failed() bool {
	return state.err != nil
}

/*
Err returns the first recorded error, or nil if the state is clean.
*/
func (state *State) Err() error {
	return state.err
}

/*
Reset clears the error state for a new call scope. Does not affect
the recovery goroutine — use Heal for that.
*/
func (state *State) Reset() {
	state.err = nil
}

/*
Heal clears the error state and marks recovery as complete, allowing
the recovery function to be triggered again on a future error. Called
by the recovery function when the object has been successfully restored.
*/
func (state *State) Heal() {
	state.err = nil
	state.healing.Store(false)
}

/*
Guard executes fn only if the state is clean. If the state has already
failed, it skips execution and returns the zero value of T — avoiding
the panic/recover overhead of SafeMust on a broken instance.

When fn returns an error or panics, the state is marked failed via
Handle and all subsequent Guard/GuardVoid calls on the same state
will short-circuit.
*/
func Guard[T any](state *State, fn func() (T, error)) T {
	var zero T

	if state.Failed() {
		return zero
	}

	return SafeMust(fn, state.Handle)
}

/*
GuardVoid executes fn only if the state is clean. Void variant for
functions that return only an error.
*/
func GuardVoid(state *State, fn func() error) {
	if state.Failed() {
		return
	}

	SafeMustVoid(fn, state.Handle)
}

/*
StateWithRecovery injects a recovery function that is called exactly
once (in a goroutine) when the first error is recorded. The recovery
function should attempt to restore the object and call state.Heal()
on success. If recovery fails, the state stays broken and the circuit
stays open until external intervention.
*/
func StateWithRecovery(fn func(*State)) stateOpts {
	return func(state *State) {
		state.recovery = fn
	}
}

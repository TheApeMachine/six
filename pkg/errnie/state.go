package errnie

import (
	"context"
	"fmt"
	"io"
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

When StateWithContext is provided, Handle cancels the state's context
on first error. Work bound to state.Ctx() sees cancellation and can
abort, allowing reschedule elsewhere. Heal renews the context for the
next cycle. Supports distributed failover: faulty node cycles out,
work reschedules to another node, recovery restores this node.

	state := errnie.NewState("spatial-index",
		errnie.StateWithContext(parentCtx),
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
	err       atomic.Pointer[error]
	context   string
	recovery  func(*State)
	healing   atomic.Bool
	parentCtx context.Context
	ctx       context.Context
	cancel    context.CancelFunc
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

	errPtr := new(error)
	*errPtr = wrapped

	if state.err.CompareAndSwap(nil, errPtr) {
		if state.cancel != nil {
			state.cancel()
		}
	}
}

/*
Failed reports whether the state has recorded an error.
*/
func (state *State) Failed() bool {
	return state.err.Load() != nil
}

/*
Err returns the first recorded error, or nil if the state is clean.
*/
func (state *State) Err() error {
	p := state.err.Load()
	if p == nil {
		return nil
	}

	return *p
}

/*
Reset clears the error state for a new call scope. Does not affect
the recovery goroutine — use Heal for that.
*/
func (state *State) Reset() {
	state.err.Store(nil)
}

/*
Heal clears the error state and marks recovery as complete, allowing
the recovery function to be triggered again on a future error. Called
by the recovery function when the object has been successfully restored.
*/
func (state *State) Heal() {
	state.err.Store(nil)
	state.healing.Store(false)

	if state.parentCtx != nil {
		state.ctx, state.cancel = context.WithCancel(state.parentCtx)
	}
}

/*
Ctx returns the state's context for cancellation propagation. Cancelled
on Handle; renewed on Heal. Nil if State was not created with StateWithContext.
*/
func (state *State) Ctx() context.Context {
	if state.ctx != nil {
		return state.ctx
	}
	return context.Background()
}

/*
Guard executes fn only if the state is clean. If the state has already
failed, it skips execution and returns the zero value of T.

Errors returned by fn are routed to Handle as plain data — no panic,
no stack unwinding. A deferred recover catches genuine runtime faults
(nil-pointer, out-of-bounds) so they degrade the state instead of
crashing the process.
*/
func Guard[T any](state *State, fn func() (T, error)) T {
	var zero T

	if state.Failed() {
		return zero
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	value, err := fn()

	if err != nil && err != io.EOF {
		state.Handle(err)
		return zero
	}

	return value
}

/*
GuardVoid executes fn only if the state is clean. Void variant for
functions that return only an error. No panic on routine errors.
*/
func GuardVoid(state *State, fn func() error) {
	if state.Failed() {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	err := fn()

	if err != nil && err != io.EOF {
		state.Handle(err)
	}
}

/*
Guard2 executes fn only if the state is clean. Returns two values.
No panic on routine errors.
*/
func Guard2[T any, U any](state *State, fn func() (T, U, error)) (T, U) {
	var (
		z1 T
		z2 U
	)

	if state.Failed() {
		return z1, z2
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	v1, v2, err := fn()

	if err != nil && err != io.EOF {
		state.Handle(err)
		return z1, z2
	}

	return v1, v2
}

/*
Guard3 executes fn only if the state is clean. Returns three values.
No panic on routine errors.
*/
func Guard3[T any, U any, V any](state *State, fn func() (T, U, V, error)) (T, U, V) {
	var (
		z1 T
		z2 U
		z3 V
	)

	if state.Failed() {
		return z1, z2, z3
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	v1, v2, v3, err := fn()

	if err != nil && err != io.EOF {
		state.Handle(err)
		return z1, z2, z3
	}

	return v1, v2, v3
}

/*
GuardCtx executes fn with the state's context only if the state is clean
and the context is not cancelled. No panic on routine errors.
*/
func GuardCtx[T any](state *State, fn func(context.Context) (T, error)) T {
	var zero T

	if state.Failed() {
		return zero
	}

	ctx := state.Ctx()
	if ctx.Err() != nil {
		return zero
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	value, err := fn(ctx)

	if err != nil && err != io.EOF {
		state.Handle(err)
		return zero
	}

	return value
}

/*
GuardVoidCtx executes fn with the state's context. Void variant.
No panic on routine errors.
*/
func GuardVoidCtx(state *State, fn func(context.Context) error) {
	if state.Failed() {
		return
	}

	ctx := state.Ctx()
	if ctx.Err() != nil {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	err := fn(ctx)

	if err != nil && err != io.EOF {
		state.Handle(err)
	}
}

/*
Guard2Ctx executes fn with the state's context. Returns two values.
No panic on routine errors.
*/
func Guard2Ctx[T any, U any](state *State, fn func(context.Context) (T, U, error)) (T, U) {
	var (
		z1 T
		z2 U
	)

	if state.Failed() {
		return z1, z2
	}

	ctx := state.Ctx()
	if ctx.Err() != nil {
		return z1, z2
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	v1, v2, err := fn(ctx)

	if err != nil && err != io.EOF {
		state.Handle(err)
		return z1, z2
	}

	return v1, v2
}

/*
Guard3Ctx executes fn with the state's context. Returns three values.
No panic on routine errors.
*/
func Guard3Ctx[T any, U any, V any](state *State, fn func(context.Context) (T, U, V, error)) (T, U, V) {
	var (
		z1 T
		z2 U
		z3 V
	)

	if state.Failed() {
		return z1, z2, z3
	}

	ctx := state.Ctx()
	if ctx.Err() != nil {
		return z1, z2, z3
	}

	defer func() {
		if r := recover(); r != nil {
			state.Handle(defaultSafeGuard.recoverToError(r))
		}
	}()

	v1, v2, v3, err := fn(ctx)

	if err != nil && err != io.EOF {
		state.Handle(err)
		return z1, z2, z3
	}

	return v1, v2, v3
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

/*
StateWithContext binds a cancellable context to the state. On Handle,
the context is cancelled so work bound to Ctx() can abort and reschedule.
On Heal, a fresh context is created for the next cycle.
*/
func StateWithContext(parent context.Context) stateOpts {
	return func(state *State) {
		if parent == nil {
			parent = context.Background()
		}
		state.parentCtx = parent
		state.ctx, state.cancel = context.WithCancel(parent)
	}
}

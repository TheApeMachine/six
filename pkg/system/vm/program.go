package vm

import (
	"capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Program wraps a capnp Future that resolves to a Struct. Run awaits it and
extracts the result type T via the caller's extract function.
*/
type Program[T any] struct {
	future  capnp.Future
	release capnp.ReleaseFunc
	state   *errnie.State
}

/*
NewProgram wraps a capnp Future and its release func. Call Run to await and extract.
*/
func NewProgram[T any](future capnp.Future, release capnp.ReleaseFunc) *Program[T] {
	return &Program[T]{
		future:  future,
		release: release,
		state:   errnie.NewState("vm/program"),
	}
}

/*
Run awaits the capnp Future and panics if not implemented. Use Call for actual RPC.
*/
func (program *Program[T]) Run() (T, error) {
	panic("not implemented")
}

/*
futureWithStruct is a constraint for capnp futures that resolve to a Struct.
*/
type futureWithStruct[T any] interface {
	Struct() (T, error)
}

/*
Call awaits a capnp RPC, releases resources, and extracts the result.
*/
func Call[R, S any](future futureWithStruct[S], release capnp.ReleaseFunc, extract func(S) (R, error)) (R, error) {
	defer release()
	s := errnie.Guard(errnie.NewState("vm/program"), func() (S, error) {
		return future.Struct()
	})

	return extract(s)
}

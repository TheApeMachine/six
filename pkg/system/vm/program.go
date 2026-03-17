package vm

import (
	"capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
Program is a sequence of operations to be executed by the Machine.
*/
type Program[T any] struct {
	future  capnp.Future
	release capnp.ReleaseFunc
	state   *errnie.State
}

/*
NewProgram creates a new Program.
*/
func NewProgram[T any](future capnp.Future, release capnp.ReleaseFunc) *Program[T] {
	return &Program[T]{
		future:  future,
		release: release,
		state:   errnie.NewState("vm/program"),
	}
}

/*
Run executes the Program.
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
	s, err := future.Struct()
	if err != nil {
		var z R
		return z, err
	}
	return extract(s)
}

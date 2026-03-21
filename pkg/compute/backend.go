package compute

import (
	"fmt"
	"io"

	"github.com/theapemachine/six/pkg/system/transport"
)

/*
Backend is a transport.Stream. Data flows in via Write, the Stream's
registered operations mutate/route/dispatch it, and results flow out
via Read. Routing is a side-effect on the operations, not a special
type. Everything is io.ReadWriter, so everything fits onto everything
else.

	io.Copy(backend, program)
	io.Copy(output, backend)

Or with the compositional primitives:

	transport.NewPipeline(program, backend, output)
	transport.NewFlipFlop(program, backend)
	transport.NewFeedback(backend, program)
*/
type Backend struct {
	*transport.Stream
}

type backendOpts func(*Backend)

/*
NewBackend creates a compute backend. The caller must supply a non-nil
transport.Stream via BackendWithOperations (or by setting Stream before
options return).
*/
func NewBackend(opts ...backendOpts) (*Backend, error) {
	backend := &Backend{}

	for _, opt := range opts {
		opt(backend)
	}

	if backend.Stream == nil {
		return nil, fmt.Errorf(
			"compute: Backend.Stream is nil; configure a Stream with BackendWithOperations",
		)
	}

	return backend, nil
}

/*
BackendWithOperations registers inline transforms on the Stream.
Each operation is an io.ReadWriteCloser that sees every chunk flowing
through. This is where routing, kernel dispatch, or any side-effect
lives.
*/
func BackendWithOperations(ops ...io.ReadWriteCloser) backendOpts {
	return func(backend *Backend) {
		backend.Stream = transport.NewStream(
			transport.WithOperations(ops...),
		)
	}
}

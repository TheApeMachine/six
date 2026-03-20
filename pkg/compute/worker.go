package compute

import (
	"io"

	"github.com/theapemachine/six/pkg/system/transport"
)

/*
Operation tells the Worker which direction it is moving data. Input
gathers bytes into the Stream (accumulating a program/loader). Output
drains the Stream (emitting results). The direction is just a label—
io.Copy already knows which end is the reader and which is the writer.
The flag exists so a Stream operation can branch on it when the same
Worker participates in both halves of a FlipFlop.
*/
type Operation uint8

const (
	Input Operation = iota
	Output
)

/*
Worker is a transport.Stream with a directional flag. That is all it is.
The real power comes from the operations registered on the Stream: they
mutate, route, or dispatch the bytes that flow through.

	worker := compute.NewWorker(
	    compute.WorkerWithOperation(compute.Input),
	    compute.WorkerWithOperations(router),
	)
	io.Copy(worker, program)
*/
type Worker struct {
	*transport.Stream
	operation Operation
}

type workerOpts func(*Worker)

/*
NewWorker wraps a Stream with an operation direction.
*/
func NewWorker(opts ...workerOpts) *Worker {
	worker := &Worker{}

	for _, opt := range opts {
		opt(worker)
	}

	if worker.Stream == nil {
		worker.Stream = transport.NewStream()
	}

	return worker
}

/*
WorkerWithOperation sets the directional flag.
*/
func WorkerWithOperation(op Operation) workerOpts {
	return func(worker *Worker) {
		worker.operation = op
	}
}

/*
WorkerWithOperations wires inline transforms on the Stream.
*/
func WorkerWithOperations(ops ...io.ReadWriteCloser) workerOpts {
	return func(worker *Worker) {
		worker.Stream = transport.NewStream(
			transport.WithOperations(ops...),
		)
	}
}

package transport

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
Pump is a workflow component that creates a continuous feedback loop in a pipeline.
It reads from the pipeline's output and feeds it back into its input, creating
an infinite processing cycle that can be stopped via stopping.
*/
type Pump struct {
	pipeline    io.ReadWriteCloser
	passthrough *Stream
	closeOnce   sync.Once
	wg          sync.WaitGroup
	stopping    atomic.Uint32
}

/*
NewPump creates a new Pump instance that wraps the provided pipeline.
It sets up a buffer that continuously processes data through the pipeline
using a FlipFlop pattern until Close signals shutdown.

Parameters:
  - pipeline: The io.ReadWriteCloser that will be pumped in a loop

Returns:
  - *Pump: A new Pump instance ready to create a feedback loop
*/
func NewPump(pipeline io.ReadWriteCloser) *Pump {
	pump := &Pump{
		pipeline:    pipeline,
		passthrough: NewStream(),
	}

	pump.wg.Add(1)
	go pump.run()

	return pump
}

/*
run keeps flipping artifacts through the pipeline until shutdown.
*/
func (pump *Pump) run() {
	defer pump.wg.Done()

	for {
		if pump.stopping.Load() != 0 {
			return
		}

		if err := NewFlipFlop(pump.pipeline, pump.passthrough); err != nil {
			errnie.Error(err)

			return
		}
	}
}

/*
Read implements the io.Reader interface.
It delegates the read operation to the underlying pipeline.

Parameters:
  - p: Byte slice to read data into

Returns:
  - n: Number of bytes read
  - err: Any error that occurred during reading
*/
func (pump *Pump) Read(p []byte) (n int, err error) {
	return pump.pipeline.Read(p)
}

/*
Write implements the io.Writer interface.
It delegates the write operation to the underlying pipeline.

Parameters:
  - p: Byte slice containing data to write

Returns:
  - n: Number of bytes written
  - err: Any error that occurred during writing
*/
func (pump *Pump) Write(p []byte) (n int, err error) {
	return pump.pipeline.Write(p)
}

/*
Close implements the io.Closer interface.
It signals shutdown via the done channel and closes the underlying pipeline.

Returns:
  - error: Any error that occurred during closure
*/
func (pump *Pump) Close() error {
	var closeErr error

	pump.closeOnce.Do(func() {
		pump.stopping.Store(1)

		passthroughErr := pump.passthrough.Close()
		pipelineErr := pump.pipeline.Close()
		closeErr = errors.Join(pipelineErr, passthroughErr)

		pump.wg.Wait()
	})

	return closeErr
}

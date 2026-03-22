package transport

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

/*
Pump creates a continuous feedback loop in a pipeline.
It reads from the pipeline's output and feeds it back into its input,
creating an infinite processing cycle that can be stopped via stopping.
*/
type Pump struct {
	pipeline    io.ReadWriteCloser
	passthrough *Stream
	closeOnce   sync.Once
	wg          sync.WaitGroup
	stopping    atomic.Uint32
}

/*
NewPump creates a new Pump that wraps the provided pipeline.
It sets up a buffer that continuously processes data through the pipeline
using a FlipFlop pattern until Close signals shutdown.
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
			return
		}
	}
}

/*
Read implements io.Reader, delegating to the pipeline.
*/
func (pump *Pump) Read(p []byte) (n int, err error) {
	return pump.pipeline.Read(p)
}

/*
Write implements io.Writer, delegating to the pipeline.
*/
func (pump *Pump) Write(p []byte) (n int, err error) {
	return pump.pipeline.Write(p)
}

/*
Close signals shutdown and closes the pipeline.
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

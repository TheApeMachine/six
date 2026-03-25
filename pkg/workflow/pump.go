package workflow

import (
	"errors"
	"io"
	"sync"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Pump holds a blocking ring buffer and exposes:
  - Loop: io.ReadWriter for pipeline heads (e.g. MergeSource) to read fed-back bytes
  - Sink: io.Writer to include on Feedback's backward path (often with MultiWriter)
  - Attach: bind the inner pipeline once the head is built with Loop()

Reads and writes on Pump itself delegate to the attached pipeline.
*/
// defaultPumpFrameCount is the number of Value frames the ring buffer should hold
// (each frame is primitive.ByteSize). Holding multiple frames avoids blocking Sink
// writes when the read side is slow.
const defaultPumpFrameCount = 8

type Pump struct {
	mu       sync.RWMutex
	pipeline io.ReadWriter
	buffer   *ringbuffer.RingBuffer
	loop     *pipeLoop // loop.pr / loop.pw are the paired PipeReader / PipeWriter on buffer
}

type pipeLoop struct {
	pr *ringbuffer.PipeReader
	pw *ringbuffer.PipeWriter
}

func (pl *pipeLoop) Read(p []byte) (int, error) {
	return pl.pr.Read(p)
}

func (pl *pipeLoop) Write(p []byte) (int, error) {
	return pl.pw.Write(p)
}

// NewPump builds a pump whose ring buffer holds defaultPumpFrameCount frames.
func NewPump() *Pump {
	return NewPumpWithFrameCount(0)
}

// NewPumpWithFrameCount sets how many full Value frames fit in the ring (minimum 2).
// Pass 0 to use defaultPumpFrameCount.
func NewPumpWithFrameCount(frameCount int) *Pump {
	if frameCount < 2 {
		frameCount = defaultPumpFrameCount
	}

	size := frameCount * primitive.ByteSize
	rb := ringbuffer.New(size)
	pr, pw := rb.Pipe()
	pl := &pipeLoop{pr: pr, pw: pw}

	return &Pump{
		buffer: rb,
		loop:   pl,
	}
}

// Loop is the substrate MergeSource (or similar) reads after dataset/inject is done.
func (pump *Pump) Loop() io.ReadWriter {
	pump.mu.RLock()
	defer pump.mu.RUnlock()
	return pump.loop
}

// Sink writes bytes into the feedback ring; pair with Feedback's backward writer
// (e.g. io.MultiWriter(prompt, pump.Sink())).
func (pump *Pump) Sink() io.Writer {
	pump.mu.RLock()
	defer pump.mu.RUnlock()
	if pump.loop == nil {
		return nil
	}
	return pump.loop.pw
}

// Attach binds the pipeline built with this pump's Loop() and Sink().
func (pump *Pump) Attach(pipeline io.ReadWriter) {
	pump.mu.Lock()
	defer pump.mu.Unlock()
	pump.pipeline = pipeline
}

func (pump *Pump) Read(p []byte) (n int, err error) {
	pump.mu.RLock()
	pipe := pump.pipeline
	pump.mu.RUnlock()
	if pipe == nil {
		return 0, errors.New("workflow: pump: Attach pipeline before Read")
	}
	return pipe.Read(p)
}

func (pump *Pump) Write(p []byte) (n int, err error) {
	pump.mu.RLock()
	pipe := pump.pipeline
	pump.mu.RUnlock()
	if pipe == nil {
		return 0, errors.New("workflow: pump: Attach pipeline before Write")
	}
	return pipe.Write(p)
}

// Close shuts down the ring pipe ends and clears references. Safe to call more than once.
func (pump *Pump) Close() error {
	if pump == nil {
		return nil
	}

	pump.mu.Lock()
	defer pump.mu.Unlock()

	var errs error
	if pump.loop != nil {
		if pump.loop.pr != nil {
			if err := pump.loop.pr.Close(); err != nil {
				errs = errors.Join(errs, err)
			}
		}
		if pump.loop.pw != nil {
			if err := pump.loop.pw.Close(); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}

	pump.loop = nil
	pump.buffer = nil
	pump.pipeline = nil

	return errs
}

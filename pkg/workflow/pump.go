package workflow

import (
	"errors"
	"io"

	"github.com/smallnest/ringbuffer"
)

/*
Pump holds a blocking ring buffer and exposes:
  - Loop: io.ReadWriter for pipeline heads (e.g. MergeSource) to read fed-back bytes
  - Sink: io.Writer to include on Feedback's backward path (often with MultiWriter)
  - Attach: bind the inner pipeline once the head is built with Loop()

Reads and writes on Pump itself delegate to the attached pipeline.
*/
type Pump struct {
	pipeline io.ReadWriter
	pw       *ringbuffer.PipeWriter
	buffer   *ringbuffer.RingBuffer
	loop     *pipeLoop
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

func NewPump() *Pump {
	rb := ringbuffer.New(1024)
	pr, pw := rb.Pipe()
	pl := &pipeLoop{pr: pr, pw: pw}

	return &Pump{
		pw:     pw,
		buffer: rb,
		loop:   pl,
	}
}

// Loop is the substrate MergeSource (or similar) reads after dataset/inject is done.
func (pump *Pump) Loop() io.ReadWriter {
	return pump.loop
}

// Sink writes bytes into the feedback ring; pair with Feedback's backward writer
// (e.g. io.MultiWriter(prompt, pump.Sink())).
func (pump *Pump) Sink() io.Writer {
	return pump.pw
}

// Attach binds the pipeline built with this pump's Loop() and Sink().
func (pump *Pump) Attach(pipeline io.ReadWriter) {
	pump.pipeline = pipeline
}

func (pump *Pump) Read(p []byte) (n int, err error) {
	if pump.pipeline == nil {
		return 0, errors.New("workflow: pump: Attach pipeline before Read")
	}
	return pump.pipeline.Read(p)
}

func (pump *Pump) Write(p []byte) (n int, err error) {
	if pump.pipeline == nil {
		return 0, errors.New("workflow: pump: Attach pipeline before Write")
	}
	return pump.pipeline.Write(p)
}

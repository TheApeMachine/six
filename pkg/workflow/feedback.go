package workflow

import (
	"errors"
	"io"
)

// DrainWriter wraps an io.Writer so each Write(p) call loops until all of p is
// accepted or the inner writer errors. Use this when composing *primitive.Value
// (or similar) with io.MultiWriter or io.Copy: those require nw == len(p) on
// success, while Value.Write may return a smaller n at chunk boundaries without error.
type DrainWriter struct {
	W io.Writer
}

func (d DrainWriter) Write(p []byte) (int, error) {
	if d.W == nil {
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.ErrShortWrite
	}
	if len(p) == 0 {
		return 0, nil
	}
	total := 0
	for total < len(p) {
		n, err := d.W.Write(p[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

/*
Feedback implements a bidirectional data flow mechanism that allows simultaneous forward
and backward streaming of data in a workflow pipeline. It wraps io.ReadWriter and io.Writer
interfaces to create a tee-based streaming pattern.

The type is particularly useful in scenarios where:
- Data needs to be processed in a forward direction while maintaining a copy
- Responses need to be captured and sent backwards in the pipeline

Structure:
- forward: Primary data channel that implements both reading and writing
- backward: Secondary channel for writing copies of the data
- tee: A TeeReader that automatically copies data from forward to backward during reads

```go

	pipeline := workflow.NewPipeline(
	    dataset,              // Input source
	    workflow.NewFeedback(
	        seed,             // Forward stream (backend)
	        prompt,           // Backward stream (seed)
	    ),
	    backend,              // Output destination
	)

	// When data flows through this pipeline:
	// 1. Dataset sends prompt to the seed
	// 2. Seed's response streams forward to the backend
	// 3. Simultaneously, the response is fed back to the dataset
	// 4. Backend processes the response for output
	// This creates a self-updating substrate while maintaining output flow

```
*/
type Feedback struct {
	forward  io.ReadWriter
	backward io.Writer
	tee      io.Reader
}

/*
NewFeedback creates a new Feedback instance that manages bidirectional data flow.

Parameters:
  - forward: Primary ReadWriter for the main data flow
  - backward: Writer for the copy/feedback stream

Returns:
  - *Feedback: Configured Feedback instance with tee reading set up
*/
func NewFeedback(forward io.ReadWriter, backward io.Writer) *Feedback {
	return &Feedback{
		forward:  forward,
		backward: backward,
		tee:      io.TeeReader(forward, backward),
	}
}

/*
Read implements io.Reader. It reads from the tee reader, which automatically
copies all read data to the backward writer while returning it from the
forward reader.

Parameters:
  - p: Byte slice to read data into

Returns:
  - n: Number of bytes read
  - err: Any error that occurred during reading
*/
func (feedback *Feedback) Read(p []byte) (n int, err error) {
	return feedback.tee.Read(p)
}

/*
Write implements io.Writer. It writes data to the forward writer and updates
the tee reader to reflect the new content.

Parameters:
  - p: Byte slice containing data to write

Returns:
  - n: Number of bytes written
  - err: Any error that occurred during writing
*/
func (feedback *Feedback) Write(p []byte) (n int, err error) {
	// TeeReader reads from forward; writes only hit forward — the tee is not rebuilt here.
	if n, err = feedback.forward.Write(p); err != nil {
		return n, err
	}

	return n, nil
}

/*
Close implements io.Closer. It attempts to close both forward and backward
components if they implement io.Closer.

Returns:
  - error: Any error that occurred while closing either component
*/
func (feedback *Feedback) Close() error {
	var closeErrs error

	if closer, ok := feedback.forward.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			closeErrs = errors.Join(closeErrs, err)
		}
	}

	if closer, ok := feedback.backward.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			closeErrs = errors.Join(closeErrs, err)
		}
	}

	return closeErrs
}

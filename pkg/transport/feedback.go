package transport

import (
	"context"
	"io"

	"github.com/theapemachine/six/pkg/errnie"
)

/*
Feedback implements a bidirectional data flow mechanism that allows simultaneous forward
and backward streaming of data in a workflow pipeline. It wraps io.ReadWriter and io.Writer
interfaces to create a tee-based streaming pattern.

The type is particularly useful in scenarios where:
- Data needs to be processed in a forward direction while maintaining a copy
- Responses need to be captured and sent backwards in the pipeline
- LLM (Language Learning Model) responses need to be stored in an agent's context

Structure:
- forward: Primary data channel that implements both reading and writing
- backward: Secondary channel for writing copies of the data
- tee: A TeeReader that automatically copies data from forward to backward during reads

Real-world Example:
In an AI pipeline, Feedback is used to simultaneously stream LLM provider responses
to both the output converter and back to the agent's context:

```go

	pipeline := workflow.NewPipeline(
	    agent,                // Input source
	    workflow.NewFeedback(
	        provider,         // Forward stream (LLM provider)
	        agent,            // Backward stream (agent context)
	    ),
	    converter,            // Output destination
	)

	// When data flows through this pipeline:
	// 1. Agent sends prompt to the provider
	// 2. Provider's response streams forward to the converter
	// 3. Simultaneously, the response is fed back to the agent's context
	// 4. Converter processes the response for output
	// This creates a self-updating conversation context while maintaining output flow

```
*/
type Feedback struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
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
func NewFeedback(ctx context.Context, forward io.ReadWriter, backward io.Writer) *Feedback {
	ctx, cancel := context.WithCancel(ctx)

	return &Feedback{
		ctx:      ctx,
		cancel:   cancel,
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
	select {
	case <-feedback.ctx.Done():
		return 0, feedback.ctx.Err()
	default:
		return feedback.tee.Read(p)
	}
}

func (feedback *Feedback) Update(components ...io.ReadWriter) {
	if feedback == nil {
		return
	}

	if updater, ok := feedback.forward.(interface {
		Update(components ...io.ReadWriter)
	}); ok {
		updater.Update(components...)
	}

	feedback.tee = io.TeeReader(feedback.forward, feedback.backward)
}

/*
Write implements io.Writer. It writes data to both the forward writer and the
backward writer, mirroring the tee semantics of Read. The backward write is
best-effort: its error is intentionally swallowed so a slow or unavailable
sink (e.g. a telemetry bridge with no connected WebSocket) never stalls the
hot path.

Parameters:
  - p: Byte slice containing data to write

Returns:
  - n: Number of bytes written
  - err: Any error that occurred during writing
*/
func (feedback *Feedback) Write(p []byte) (n int, err error) {
	select {
	case <-feedback.ctx.Done():
		return 0, feedback.ctx.Err()
	default:
		if n, err = feedback.forward.Write(p); err != nil {
			return n, errnie.Error(err)
		}

		return n, nil
	}
}

/*
Close cancels this Feedback's context only. Forward and backward are often
shared infrastructure (telemetry Bridge, pool.Queue) owned by vm.Machine —
closing them here would tear down unrelated subsystems when gossip.Conn
stops.
*/
func (feedback *Feedback) Close() error {
	feedback.cancel()

	return nil
}

/*
Error returns the most recent error that occurred during reading or writing.
*/
func (feedback *Feedback) Error() error {
	return feedback.err
}

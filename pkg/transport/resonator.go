package transport

import (
	"io"

	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

/*
Resonator drives the feedback loop between a prompt and a composition
pipeline. It holds the original prompt as a fixed reference, computes
XOR residuals between the prompt and each cycle's output, checks
convergence via PopCount, and detects motor orbits.

When the residual reaches zero, the prompt is fully satisfied — the
composition has converged. When a motor orbit is detected (same residual
visited twice), the current trajectory is exhausted and the system
should branch.

The trace of all intermediate outputs is accumulated for Reify, which
collapses the motor chain into a single tool Value that can be written
back to the corpus.
*/
type Resonator struct {
	prompt    *primitive.Value
	residual  *primitive.Value
	trace     []*primitive.Value
	visited   map[primitive.Value]struct{}
	started   bool
	converged bool
}

/*
NewResonator creates a feedback state manager anchored to the given
prompt. The prompt defines the target state: the feedback loop converges
when an output's XOR with the prompt reaches zero PopCount.
*/
func NewResonator(prompt *primitive.Value) *Resonator {
	return &Resonator{
		prompt:  prompt,
		visited: make(map[primitive.Value]struct{}),
	}
}

/*
Read emits the current navigation signal. On the first call it emits
the prompt itself (the initial state for the pipeline to process). On
subsequent calls it emits the XOR residual from the most recent Write.
Returns io.EOF when the residual reaches zero (converged) or when no
residual is available.
*/
func (resonator *Resonator) Read(p []byte) (n int, err error) {
	if resonator.converged {
		return 0, io.EOF
	}

	if !resonator.started {
		resonator.started = true
		n, _ = resonator.prompt.Read(p)

		return n, nil
	}

	if resonator.residual == nil {
		return 0, io.EOF
	}

	n, _ = resonator.residual.Read(p)
	resonator.residual = nil

	return n, nil
}

/*
Write receives a composition output, computes XOR(prompt, output) as
the lattice distance, and checks convergence. PopCount zero means the
output fully satisfies the prompt. A repeated residual means the motor
has entered an orbit — the trajectory is exhausted and the caller
should branch to a different region.
*/
func (resonator *Resonator) Write(p []byte) (n int, err error) {
	if len(p) < primitive.ByteSize {
		return 0, primitive.ErrShortValue
	}

	output := primitive.NewValueFromBytes(p)
	resonator.trace = append(resonator.trace, output)

	residual := primitive.NewValue()
	operation.XOR(resonator.prompt[:], output[:], residual[:])
	residual.Clamp()

	if residual.PopCount() == 0 {
		resonator.converged = true

		return primitive.ByteSize, nil
	}

	if _, seen := resonator.visited[*residual]; seen {
		return 0, ResonatorOrbitError
	}

	resonator.visited[*residual] = struct{}{}
	resonator.residual = residual

	return primitive.ByteSize, nil
}

/*
Close is a no-op; the Resonator has no external resources.
Call Reify to extract the tool Value before discarding.
*/
func (resonator *Resonator) Close() error {
	return nil
}

/*
Converged reports whether the feedback loop reached zero residual.
*/
func (resonator *Resonator) Converged() bool {
	return resonator.converged
}

/*
Trace returns the sequence of output Values received during the
feedback loop, in order. Each Value contributed a motor to the
navigation chain.
*/
func (resonator *Resonator) Trace() []*primitive.Value {
	return resonator.trace
}

/*
Reify collapses the accumulated motor trace into a single affine
transform and encodes it as a Value. The composed motor represents
the entire navigation path that solved the prompt. Written back to
the corpus, it becomes a reusable tool: future prompts with high
GCD against the tool Value navigate to it automatically, and its
motor shortcuts the multi-step composition.

The tool Value has two bits set at positions (scale, translate) of
the composed motor. Its own derived motor encodes the transform as
a structural fingerprint on the prime lattice.
*/
func (resonator *Resonator) Reify() *primitive.Value {
	if len(resonator.trace) == 0 {
		return primitive.NewValue()
	}

	scale, translate := uint16(1), uint16(0)

	for _, value := range resonator.trace {
		stepScale, stepTranslate := value.Motor()
		scale, translate = primitive.ComposeMotor(
			scale, translate, stepScale, stepTranslate,
		)
	}

	tool := primitive.NewValue()
	tool.Set(int(scale))
	tool.Set(int(translate))

	return tool
}

/*
ResonatorError is a typed error for feedback loop failures.
*/
type ResonatorError string

const (
	ResonatorOrbitError ResonatorError = "resonator: motor entered orbit, trajectory exhausted"
)

/*
Error implements the error interface for ResonatorError.
*/
func (resonatorErr ResonatorError) Error() string {
	return string(resonatorErr)
}

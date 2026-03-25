package workflow

import (
	"bytes"
	"errors"
	"io"
)

// LoopReadGater is implemented by pipeline heads that can temporarily disable
// reads from the feedback loop during Pipeline's first processed pass (see Pipeline.Read).
type LoopReadGater interface {
	SetAllowLoopReads(ok bool)
}

/*
MergeSource multiplexes several logical inputs into one io.Reader for pipeline
heads:
 1. Inject buffer (written via Write — e.g. host prompt injection)
 2. Optional primary stream (e.g. dataset), until EOF
 3. Loop substrate (io.ReadWriter — e.g. *primitive.Value fed by Feedback)

After the primary stream is exhausted, reads continue from Loop so an
always-on system can consume feedback-fed state on subsequent ticks.
*/
type MergeSource struct {
	inject           bytes.Buffer
	dataset          io.Reader
	loop             io.ReadWriter
	datasetExhausted bool
	// allowLoopReads controls whether readLoop may block on the feedback ring.
	// During Pipeline's first pass, the head is copied forward before Feedback
	// runs; reading the loop then would deadlock (empty ring, Sink not fed yet).
	// NewMergeSource sets this true; Pipeline defers loop only for that prime.
	allowLoopReads bool
}

// NewMergeSource builds a merge reader. dataset may be nil (inject + loop only).
func NewMergeSource(dataset io.Reader, loop io.ReadWriter) *MergeSource {
	return &MergeSource{dataset: dataset, loop: loop, allowLoopReads: true}
}

// SetAllowLoopReads sets whether reads may block on the loop substrate after the
// dataset is exhausted. Disable during pipeline prime (see Pipeline.Read).
func (m *MergeSource) SetAllowLoopReads(ok bool) {
	m.allowLoopReads = ok
}

// SetDataset replaces the primary stream and clears EOF state.
func (m *MergeSource) SetDataset(dataset io.Reader) {
	m.dataset = dataset
	m.datasetExhausted = false
	m.allowLoopReads = true
}

// Write appends to the inject buffer, consumed before dataset and loop reads.
func (m *MergeSource) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	return m.inject.Write(p)
}
func (m *MergeSource) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if m.inject.Len() > 0 {
		return m.inject.Read(p)
	}
	if m.dataset != nil && !m.datasetExhausted {
		n, err = m.dataset.Read(p)
		if errors.Is(err, io.EOF) {
			m.datasetExhausted = true
			if n > 0 {
				return n, nil
			}
			return m.readLoop(p)
		}
		return n, err
	}
	return m.readLoop(p)
}
func (m *MergeSource) readLoop(p []byte) (n int, err error) {
	if m.loop == nil {
		return 0, io.EOF
	}
	if !m.allowLoopReads {
		return 0, io.EOF
	}
	return m.loop.Read(p)
}

// Close is a no-op. MergeSource does not close m.loop: the owner of that
// ReadWriter (e.g. the Pump that created it) must close it when tearing down.
// Close the dataset from the caller if it is an io.ReadCloser.
func (m *MergeSource) Close() error {
	return nil
}

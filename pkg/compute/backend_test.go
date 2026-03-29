package compute

import (
	"context"
	"testing"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

type fakeSubstrate struct{ id int }

func (fakeSubstrate) UniversalBitwise(a, b unsafe.Pointer, count int) error { return nil }
func (fakeSubstrate) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

var _ kernel.Substrate = (*fakeSubstrate)(nil)

func TestGatherBatchUsesWindow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := &Backend{
		ctx:         ctx,
		cancel:      cancel,
		jobQueue:    make(chan bitwiseJob, 4),
		batchSize:   4,
		batchWindow: 5 * time.Millisecond,
	}

	backend.jobQueue <- bitwiseJob{done: make(chan error, 1)}
	batch := backend.gatherBatch(bitwiseJob{done: make(chan error, 1)})

	if got, want := len(batch), 2; got != want {
		t.Fatalf("gatherBatch len=%d want=%d", got, want)
	}
}

func TestNextSubstrateRoundRobin(t *testing.T) {
	backend := &Backend{
		hardware: []kernel.Substrate{fakeSubstrate{id: 1}, fakeSubstrate{id: 2}},
	}

	first := backend.nextSubstrate()
	second := backend.nextSubstrate()
	third := backend.nextSubstrate()

	if first == nil || second == nil || third == nil {
		t.Fatal("nextSubstrate returned nil substrate")
	}
	if first != third {
		t.Fatal("round-robin did not wrap back to first substrate")
	}
	if first == second {
		t.Fatal("round-robin did not advance to the next substrate")
	}
}

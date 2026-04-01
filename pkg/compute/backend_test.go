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

	q := make(chan bitwiseJob, 4)
	backend := &Backend{
		ctx:         ctx,
		cancel:      cancel,
		batchSize:   4,
		batchWindow: 5 * time.Millisecond,
	}

	q <- bitwiseJob{done: make(chan error, 1)}
	batch := backend.gatherBatch(q, bitwiseJob{done: make(chan error, 1)})

	if got, want := len(batch), 2; got != want {
		t.Fatalf("gatherBatch len=%d want=%d", got, want)
	}
}

func TestPickAcceleratorPrefersLeastInflight(t *testing.T) {
	backend := &Backend{
		accel: []accelSlot{
			{sub: fakeSubstrate{id: 1}, inflight: 9, emaPerFrameNs: 1},
			{sub: fakeSubstrate{id: 2}, inflight: 1, emaPerFrameNs: 1000},
		},
	}

	if idx := backend.pickAccelerator(); idx != 1 {
		t.Fatalf("pickAccelerator: idx=%d want 1 (least inflight)", idx)
	}
}

func TestPickAcceleratorTieBreaksOnEMA(t *testing.T) {
	backend := &Backend{
		accel: []accelSlot{
			{sub: fakeSubstrate{id: 1}, inflight: 2, emaPerFrameNs: 500},
			{sub: fakeSubstrate{id: 2}, inflight: 2, emaPerFrameNs: 100},
		},
	}

	if idx := backend.pickAccelerator(); idx != 1 {
		t.Fatalf("pickAccelerator: idx=%d want 1 (lower EMA)", idx)
	}
}

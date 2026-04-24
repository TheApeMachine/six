//go:build !darwin || !cgo

package metal

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
)

var ErrUnavailable = errors.New("metal: backend unavailable")

/*
Backend is the stub for non-darwin builds. It satisfies the full
kernel.Substrate contract by delegating every call to the cross-
substrate CPU helpers — the binary still links and runs, just without
GPU acceleration.
*/
type Backend struct {
	idx      int
	inflight atomic.Int64
	emaNs    atomic.Uint64
}

type backendOption func(*Backend)

/*
NewBackend returns a stub Backend on non-darwin.
*/
func NewBackend(idx int, opts ...backendOption) *Backend {
	backend := &Backend{
		idx: idx,
	}
	for _, opt := range opts {
		opt(backend)
	}
	return backend
}

func (backend *Backend) Close() {}

/*
Available always returns zero on non-darwin.
*/
func Available() int { return 0 }

func (backend *Backend) Name() string { return "metal" }

func (backend *Backend) Pressure() (inflight int64, emaNs uint64) {
	return backend.inflight.Load(), backend.emaNs.Load()
}

func (backend *Backend) CanProfit(kind kernel.JobKind, size int) bool {
	_ = kind
	_ = size
	return false
}

func (backend *Backend) recordService(start time.Time) {
	elapsed := uint64(time.Since(start).Nanoseconds())
	for {
		old := backend.emaNs.Load()
		next := old - (old >> 3) + (elapsed >> 3)
		if old == 0 {
			next = elapsed
		}
		if backend.emaNs.CompareAndSwap(old, next) {
			return
		}
	}
}

func (backend *Backend) UniversalBitwise(a, b, dst *primitive.Value) {
	backend.inflight.Add(1)
	start := time.Now()
	defer func() {
		backend.inflight.Add(-1)
		backend.recordService(start)
	}()
	kernel.RunUniversalBitwise(a, b, dst)
}

func (backend *Backend) AssignFirstFit(
	communityORs [][primitive.AffinityWords]uint64,
	valueAffinities [][primitive.AffinityWords]uint64,
	hammingBudget uint32,
	saturationCap uint32,
) []int32 {
	out, _ := BatchFirstFit(communityORs, valueAffinities, hammingBudget, saturationCap)
	return out
}

func (backend *Backend) ExecuteCommunity(community []*primitive.Value) []*primitive.Value {
	if len(community) == 0 {
		return nil
	}

	// Just fallback to CPU for now
	return cpu.ExecuteCommunity(community)
}

/*
BatchFirstFit always reports the backend as unavailable on non-darwin.
*/
func BatchFirstFit(
	communityORs [][primitive.AffinityWords]uint64,
	valueAffinities [][primitive.AffinityWords]uint64,
	hammingBudget uint32,
	saturationCap uint32,
) ([]int32, error) {
	return nil, ErrUnavailable
}

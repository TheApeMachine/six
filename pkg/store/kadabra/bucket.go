package kadabra

import (
	"maps"
	"math"
	"math/bits"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric"
)

/*
bucketState is an immutable routing bucket snapshot swapped atomically so
peer bookkeeping never takes a per-bucket mutex.
*/
type bucketState struct {
	Entries         PeerSet
	Candidates      map[uint64]*Peer
	PreviousEntries PeerSet
	PreviousScore   float64
	ExploreNext     bool
	QueryCount      int
}

/*
Bucket holds the routing entries and candidate peers for one
XOR distance prefix in the Kadabra routing table.
*/
type Bucket struct {
	Index int
	state atomic.Pointer[bucketState]
}

func newBucketState() *bucketState {
	return &bucketState{
		Candidates:    make(map[uint64]*Peer),
		PreviousScore: math.Inf(-1),
	}
}

/*
CloneState returns a deep copy of the current bucket state for
copy-on-write CAS mutations.
*/
func (bucket *Bucket) CloneState(source *bucketState) *bucketState {
	if source == nil {
		return newBucketState()
	}

	return &bucketState{
		Entries:         append(PeerSet(nil), source.Entries...),
		Candidates:      maps.Clone(source.Candidates),
		PreviousEntries: append(PeerSet(nil), source.PreviousEntries...),
		PreviousScore:   source.PreviousScore,
		ExploreNext:     source.ExploreNext,
		QueryCount:      source.QueryCount,
	}
}

/*
CAS applies a lock-free copy-on-write mutation to the bucket state.
The mutator receives a cloned snapshot; on CAS failure it retries
with a fresh clone.
*/
func (bucket *Bucket) CAS(mut func(*bucketState)) {
	for {
		old := bucket.state.Load()

		if old == nil {
			if !bucket.state.CompareAndSwap(nil, newBucketState()) {
				continue
			}

			old = bucket.state.Load()
		}

		next := bucket.CloneState(old)
		mut(next)

		if bucket.state.CompareAndSwap(old, next) {
			return
		}
	}
}

/*
IndexFor returns the bucket index for remote relative to local
using a fixed routing bit width. When local and remote coincide
(distance zero) it returns -1 so callers can reject self-routing.
*/
func IndexFor(local uint64, remote uint64, routingBits int) int {
	if routingBits <= 0 {
		routingBits = core.Cfg.Kadabra.Bits
	}

	distance := numeric.XOR(local, remote)

	if distance == 0 {
		return -1
	}

	return bits.LeadingZeros64(distance)
}

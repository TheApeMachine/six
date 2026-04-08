package kadabra

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestNewBucketState(t *testing.T) {
	t.Parallel()

	Convey("newBucketState starts with empty candidates", t, func() {
		st := newBucketState()

		So(st.Candidates, ShouldNotBeNil)
		So(len(st.Entries), ShouldEqual, 0)
		So(st.PreviousScore, ShouldEqual, math.Inf(-1))
	})
}

func TestBucketCloneState(t *testing.T) {
	t.Parallel()

	Convey("CloneState(nil) allocates fresh state", t, func() {
		bucket := &Bucket{}

		cloned := bucket.CloneState(nil)

		So(cloned, ShouldNotBeNil)
		So(cloned.Candidates, ShouldNotBeNil)
	})

	Convey("CloneState copies Entries and query bookkeeping", t, func() {
		bucket := &Bucket{}

		source := newBucketState()
		source.QueryCount = 42
		source.ExploreNext = true
		source.Entries = PeerSet{&Peer{ID: 9, RTT: 1.5}}
		source.Candidates[9] = source.Entries[0]

		cloned := bucket.CloneState(source)

		So(cloned.QueryCount, ShouldEqual, 42)
		So(cloned.ExploreNext, ShouldBeTrue)
		So(len(cloned.Entries), ShouldEqual, 1)
		So(cloned.Entries[0].ID, ShouldEqual, 9)

		source.Candidates[8] = &Peer{ID: 8, RTT: 3}

		_, ingestsIntoClone := cloned.Candidates[8]

		So(ingestsIntoClone, ShouldBeFalse)
	})
}

func TestBucketCAS(t *testing.T) {
	t.Parallel()

	Convey("CAS applies mutator to installed state", t, func() {
		bucket := &Bucket{}

		bucket.CAS(func(st *bucketState) {
			st.QueryCount = 11
		})

		So(bucket.state.Load().QueryCount, ShouldEqual, 11)
	})
}

func TestIndexFor(t *testing.T) {
	Convey("IndexFor returns -1 when local equals remote", t, func() {
		So(IndexFor(0xcafe, 0xcafe, 64), ShouldEqual, -1)
	})

	Convey("when routingBits <= 0, It should fall back to Cfg bits", t, func() {
		original := *core.Cfg
		t.Cleanup(func() {
			*core.Cfg = original
		})

		core.Cfg.Kadabra.Bits = 64

		dist := uint64(0x8000000000000000) // single high bit

		So(IndexFor(0, dist, 0), ShouldEqual, 0)
	})
}

func BenchmarkBucketCAS(b *testing.B) {
	bucket := &Bucket{}

	b.ResetTimer()

	for b.Loop() {
		bucket.CAS(func(st *bucketState) {
			st.QueryCount++
		})
	}
}

package kadabra

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestNewPeer(t *testing.T) {
	t.Parallel()

	Convey("NewPeer wires identifiers", t, func() {
		aff := primitive.NewAffinity()

		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-peer-target", queue)

		So(nErr, ShouldBeNil)

		p := NewPeer(42, aff, node, 3.5, 2, 0, 0)

		So(p.ID, ShouldEqual, 42)
		So(p.Node, ShouldEqual, node)
		So(p.RTT, ShouldAlmostEqual, 3.5, 1e-9)
		So(p.Bucket, ShouldEqual, 2)
	})
}

func TestPeerSetMerge(t *testing.T) {
	t.Parallel()

	Convey("Merge prefers lower RTT on duplicate ID", t, func() {
		a := PeerSet{
			&Peer{ID: 1, RTT: 5},
		}
		b := PeerSet{
			&Peer{ID: 1, RTT: 2},
		}

		merged := a.Merge(b, 0)

		So(len(merged), ShouldEqual, 1)
		So(merged[0].RTT, ShouldAlmostEqual, 2, 1e-9)
	})

	Convey("Merge skips nil peers", t, func() {
		a := PeerSet{nil, &Peer{ID: 1, RTT: 1}}
		b := PeerSet{&Peer{ID: 2, RTT: 1}}

		merged := a.Merge(b, 0)

		So(len(merged), ShouldEqual, 2)
	})
}

func TestPeerSetDedup(t *testing.T) {
	t.Parallel()

	Convey("Dedup keeps first occurrence", t, func() {
		p := PeerSet{
			&Peer{ID: 1, RTT: 1},
			&Peer{ID: 1, RTT: 9},
			&Peer{ID: 2, RTT: 3},
		}

		out := p.Dedup()

		So(len(out), ShouldEqual, 2)
		So(out[0].RTT, ShouldAlmostEqual, 1, 1e-9)
	})
}

func TestPeerSetSortByID(t *testing.T) {
	t.Parallel()

	Convey("SortByID orders ascending", t, func() {
		p := PeerSet{
			&Peer{ID: 30, RTT: 0},
			&Peer{ID: 10, RTT: 0},
		}

		p.SortByID()

		So(p[0].ID, ShouldEqual, 10)
		So(p[1].ID, ShouldEqual, 30)
	})
}

func TestPeerSetSortByDistance(t *testing.T) {
	t.Parallel()

	Convey("SortByDistance orders by XOR to target", t, func() {
		target := uint64(0b1000)

		p := PeerSet{
			&Peer{ID: 0b1010, RTT: 0},
			&Peer{ID: 0b1001, RTT: 0},
		}

		p.SortByDistance(target)

		// 0b1001 is closer to 0b1000 than 0b1010.
		So(p[0].ID, ShouldEqual, 0b1001)
	})
}

func TestPeerSetAverageScores(t *testing.T) {
	t.Parallel()

	Convey("AverageScores ignores missing entries", t, func() {
		p := PeerSet{
			&Peer{ID: 1, RTT: 0},
			&Peer{ID: 2, RTT: 0},
		}

		avg, ok := p.AverageScores(map[uint64]float64{1: 4, 2: 6})

		So(ok, ShouldBeTrue)
		So(avg, ShouldAlmostEqual, 5, 1e-9)
	})

	Convey("empty score map yields false", t, func() {
		p := PeerSet{&Peer{ID: 1, RTT: 0}}

		_, ok := p.AverageScores(nil)

		So(ok, ShouldBeFalse)
	})
}

func TestPeerSetWorstScore(t *testing.T) {
	t.Parallel()

	Convey("WorstScore picks minimum", t, func() {
		p := PeerSet{
			&Peer{ID: 10, RTT: 0},
			&Peer{ID: 20, RTT: 0},
		}

		worst := p.WorstScore(map[uint64]float64{10: 0.9, 20: 0.1})

		So(worst, ShouldEqual, 20)
	})
}

func TestPeerSetNextBatch(t *testing.T) {
	t.Parallel()

	Convey("NextBatch respects limit and seen", t, func() {
		p := PeerSet{
			&Peer{ID: 1, RTT: 0},
			&Peer{ID: 2, RTT: 0},
			&Peer{ID: 3, RTT: 0},
		}

		seen := map[uint64]struct{}{1: {}}

		batch := p.NextBatch(seen, 2)

		So(len(batch), ShouldEqual, 2)
		So(batch[0].ID, ShouldEqual, 2)
		So(batch[1].ID, ShouldEqual, 3)
	})

	Convey("non-positive limit returns nil", t, func() {
		p := PeerSet{&Peer{ID: 1, RTT: 0}}

		So(p.NextBatch(nil, 0), ShouldBeNil)
	})
}

func TestConnect(t *testing.T) {
	t.Parallel()

	Convey("Connect with nil nodes is a no-op", t, func() {
		Connect(nil, nil, 1.0)
	})

	Convey("Connect registers mutual routing peers", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		left, lErr := NewNode(ctx, "kadabra-connect-a", queue)

		So(lErr, ShouldBeNil)

		right, rErr := NewNode(ctx, "kadabra-connect-b", queue)

		So(rErr, ShouldBeNil)

		Connect(left, right, 2.5)

		found := false

		for _, bucket := range left.routing.buckets {
			st := bucket.state.Load()

			if st == nil {
				continue
			}

			if cand := st.Candidates[right.ID]; cand != nil && cand.RTT == 2.5 {
				found = true

				break
			}
		}

		So(found, ShouldBeTrue)
	})

	Convey("second Connect refreshes RTT on existing peer", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		left, lErr := NewNode(ctx, "kadabra-connect-rtt-a", queue)

		So(lErr, ShouldBeNil)

		right, rErr := NewNode(ctx, "kadabra-connect-rtt-b", queue)

		So(rErr, ShouldBeNil)

		Connect(left, right, 1.0)
		Connect(left, right, 9.25)

		var updatedRtt float64

		for _, bucket := range left.routing.buckets {
			st := bucket.state.Load()

			if st == nil {
				continue
			}

			if cand := st.Candidates[right.ID]; cand != nil {
				updatedRtt = cand.RTT

				break
			}
		}

		So(updatedRtt, ShouldAlmostEqual, 9.25, 1e-9)
	})
}

func BenchmarkPeerSetMerge(b *testing.B) {
	left := PeerSet{
		&Peer{ID: 1, RTT: 1},
		&Peer{ID: 3, RTT: 2},
	}

	right := PeerSet{
		&Peer{ID: 2, RTT: 0.5},
		&Peer{ID: 1, RTT: 0.25},
	}

	target := uint64(0xfeed)

	b.ResetTimer()

	for b.Loop() {
		_ = left.Merge(right, target)
	}
}

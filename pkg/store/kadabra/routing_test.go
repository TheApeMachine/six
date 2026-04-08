package kadabra

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestClaimRecordIfNew(t *testing.T) {
	Convey("first Key admits once and duplicate Key is rejected", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-claim-first", queue)

		So(nErr, ShouldBeNil)

		rt := node.routing
		record := SequenceRecord{Key: 0xcafebabe}

		So(rt.claimRecordIfNew(record), ShouldBeTrue)
		So(rt.claimRecordIfNew(record), ShouldBeFalse)
	})

	Convey("distinct Keys each admit once", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-claim-distinct", queue)

		So(nErr, ShouldBeNil)

		rt := node.routing
		a := SequenceRecord{Key: 1}
		b := SequenceRecord{Key: 2}

		So(rt.claimRecordIfNew(a), ShouldBeTrue)
		So(rt.claimRecordIfNew(b), ShouldBeTrue)
		So(rt.claimRecordIfNew(a), ShouldBeFalse)
	})

	Convey("nil RoutingTable rejects admission", t, func() {
		var rt *RoutingTable

		So(rt.claimRecordIfNew(SequenceRecord{Key: 1}), ShouldBeFalse)
	})
}

func TestRoutingTableReleaseRecordKey(t *testing.T) {
	t.Parallel()

	Convey("releaseRecordKey lets the same Key admit again", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-release-key", queue)

		So(nErr, ShouldBeNil)

		rt := node.routing
		record := SequenceRecord{Key: 0xabc123}

		So(rt.claimRecordIfNew(record), ShouldBeTrue)
		rt.releaseRecordKey(record.Key)
		So(rt.claimRecordIfNew(record), ShouldBeTrue)
	})

	Convey("releaseRecordKey on nil RoutingTable is a no-op", t, func() {
		var rt *RoutingTable

		rt.releaseRecordKey(1)
	})
}

func TestRoutingTableClosest(t *testing.T) {
	t.Parallel()

	Convey("nil target returns nil", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-closest-nil-target", queue)

		So(nErr, ShouldBeNil)

		So(node.routing.Closest(nil, 3), ShouldBeNil)
	})

	Convey("non-positive limit returns nil", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-closest-limit", queue)

		So(nErr, ShouldBeNil)

		target := primitive.NewAffinityFromVector([primitive.AffinityWords]uint64{1, 2})

		So(node.routing.Closest(target, 0), ShouldBeNil)
	})

	Convey("Closest always includes owning node", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-closest-self", queue)

		So(nErr, ShouldBeNil)

		target := primitive.NewAffinityFromVector([primitive.AffinityWords]uint64{3, 4})

		out := node.routing.Closest(target, 4)

		So(len(out), ShouldBeGreaterThanOrEqualTo, 1)

		found := false

		for _, candidate := range out {
			if candidate == node {
				found = true

				break
			}
		}

		So(found, ShouldBeTrue)
	})
}

func BenchmarkClaimRecordIfNew(b *testing.B) {
	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, err := NewNode(ctx, "kadabra-claim-bench", queue)

	if err != nil {
		b.Fatal(err)
	}

	rt := node.routing

	b.ResetTimer()

	for idx := range b.N {
		_ = rt.claimRecordIfNew(SequenceRecord{Key: uint64(idx)})
	}
}

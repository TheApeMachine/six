package kadabra

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/pool"
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

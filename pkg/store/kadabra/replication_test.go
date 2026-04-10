package kadabra

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestStoreMeshLoadCentroidCallsExpandHandler(t *testing.T) {
	setupKadabraPrimitiveLayout(t)

	Convey("Given a saturated mesh load centroid on primary Store", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		origLimit := core.Cfg.Kadabra.ShannonLimit

		core.Cfg.Kadabra.ShannonLimit = 1

		defer func() {
			core.Cfg.Kadabra.ShannonLimit = origLimit
		}()

		node, nErr := NewNode(ctx, "kadabra-mesh-load-expand", queue)

		So(nErr, ShouldBeNil)

		var expandCalls int32

		node.SetMeshExpandHandler(func(*primitive.Affinity) bool {
			atomic.AddInt32(&expandCalls, 1)

			return true
		})

		affLow := [primitive.AffinityWords]uint64{1, 0, 0, 0, 0}
		affNext := [primitive.AffinityWords]uint64{2, 0, 0, 0, 0}

		value1, vErr := primitive.FirstSegment(primitive.NewValue([]byte("mesh-load-r1")))

		So(vErr, ShouldBeNil)

		defer value1.Close()

		record1 := SequenceRecord{
			Value:     *value1,
			Affinity:  affLow,
			Label:     "mesh-load-1",
			Publisher: node.ID,
		}

		record1.Key = record1.Hash()

		So(node.Store(record1), ShouldBeNil)

		value2, vErr2 := primitive.FirstSegment(primitive.NewValue([]byte("mesh-load-r2")))

		So(vErr2, ShouldBeNil)

		defer value2.Close()

		record2 := SequenceRecord{
			Value:     *value2,
			Affinity:  affNext,
			Label:     "mesh-load-2",
			Publisher: node.ID,
		}

		record2.Key = record2.Hash()

		So(node.Store(record2), ShouldBeNil)

		queue.Drain()

		So(atomic.LoadInt32(&expandCalls), ShouldEqual, 1)
	})
}

func BenchmarkStorePrimaryMeshLoadCentroidBlend(b *testing.B) {
	setupKadabraPrimitiveLayout(b)

	ctx := context.Background()

	queue, qErr := pool.NewQueue(ctx)

	if qErr != nil {
		b.Fatal(qErr)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, nErr := NewNode(ctx, "kadabra-mesh-load-bench", queue)

	if nErr != nil {
		b.Fatal(nErr)
	}

	aff := [primitive.AffinityWords]uint64{1, 0, 0, 0, 0}

	var serial atomic.Uint64

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		tok := strconv.AppendUint([]byte("bm-"), serial.Add(1), 10)

		value, vErr := primitive.FirstSegment(primitive.NewValue(tok))

		if vErr != nil {
			b.Fatal(vErr)
		}

		record := SequenceRecord{
			Value:     *value,
			Affinity:  aff,
			Label:     "bench",
			Publisher: node.ID,
		}

		record.Key = record.Hash()

		if storeErr := node.Store(record); storeErr != nil {
			value.Close()

			b.Fatal(storeErr)
		}

		value.Close()
	}

	queue.Drain()
}

func TestStoreReplicasReachRoutingPeers(t *testing.T) {
	Convey("Given two connected nodes sharing one queue", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		left, lErr := NewNode(ctx, "kadabra-repl-a", queue)

		So(lErr, ShouldBeNil)

		right, rErr := NewNode(ctx, "kadabra-repl-b", queue)

		So(rErr, ShouldBeNil)

		Connect(left, right, 1.0)

		value, vErr := primitive.FirstSegment(primitive.NewValue([]byte("hello")))

		So(vErr, ShouldBeNil)

		defer value.Close()

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     "demo",
			Publisher: left.ID,
		}

		record.Key = record.Hash()

		sErr := left.Store(record)

		So(sErr, ShouldBeNil)

		queue.Drain()

		ptr := right.tries.Load()

		So(ptr, ShouldNotBeNil)
		So(len(*ptr), ShouldBeGreaterThan, 0)
	})
}

func TestStoreAdmissionIdempotency(t *testing.T) {
	Convey("duplicate primary Store keeps a single routing Key", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		left, lErr := NewNode(ctx, "kadabra-idem-primary-a", queue)

		So(lErr, ShouldBeNil)

		right, rErr := NewNode(ctx, "kadabra-idem-primary-b", queue)

		So(rErr, ShouldBeNil)

		Connect(left, right, 1.0)

		value, vErr := primitive.FirstSegment(primitive.NewValue([]byte("idem-token-primary")))

		So(vErr, ShouldBeNil)

		defer value.Close()

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     "idem",
			Publisher: left.ID,
		}

		record.Key = record.Hash()

		So(left.Store(record), ShouldBeNil)
		So(left.Store(record), ShouldBeNil)

		queue.Drain()

		So(countRecordedKeys(left), ShouldEqual, 1)
	})

	Convey("duplicate StoreReplica on a peer is a no-op", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		left, lErr := NewNode(ctx, "kadabra-idem-replica-a", queue)

		So(lErr, ShouldBeNil)

		right, rErr := NewNode(ctx, "kadabra-idem-replica-b", queue)

		So(rErr, ShouldBeNil)

		Connect(left, right, 1.0)

		value, vErr := primitive.FirstSegment(primitive.NewValue([]byte("idem-token-replica")))

		So(vErr, ShouldBeNil)

		defer value.Close()

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     "idem",
			Publisher: left.ID,
		}

		record.Key = record.Hash()

		So(left.Store(record), ShouldBeNil)

		queue.Drain()

		before := countRecordedKeys(right)

		So(before, ShouldEqual, 1)

		So(right.StoreReplica(record), ShouldBeNil)
		So(right.StoreReplica(record), ShouldBeNil)

		queue.Drain()

		So(countRecordedKeys(right), ShouldEqual, before)
	})
}

func BenchmarkStoreReplicationPath(b *testing.B) {
	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	left, err := NewNode(ctx, "kadabra-bench-a", queue)

	if err != nil {
		b.Fatal(err)
	}

	right, err := NewNode(ctx, "kadabra-bench-b", queue)

	if err != nil {
		b.Fatal(err)
	}

	Connect(left, right, 1.0)

	b.ResetTimer()

	for n := range b.N {
		payload := strconv.AppendInt(append([]byte("bench-token-"), 'x'), int64(n), 10)

		value, err := primitive.FirstSegment(primitive.NewValue(payload))

		if err != nil {
			b.Fatal(err)
		}

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     "",
			Publisher: left.ID,
		}

		record.Key = record.Hash()

		err = left.Store(record)

		if err != nil {
			_ = value.Close()

			b.Fatal(err)
		}

		queue.Drain()

		_ = value.Close()
	}
}

func TestSequenceRecordHashMatchesStringForm(t *testing.T) {
	Convey("Hash matches FNV over label, separator, and token String()", t, func() {
		value, err := primitive.FirstSegment(primitive.NewValue([]byte("hash-check")))

		So(err, ShouldBeNil)

		defer value.Close()

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     "lbl",
			Publisher: 1,
		}

		hasher := fnv.New64a()

		_, _ = hasher.Write([]byte(record.Label))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(record.Value.String()))

		So(record.Hash(), ShouldEqual, hasher.Sum64())
	})
}

func countRecordedKeys(node *Node) int {
	if node == nil || node.routing == nil {
		return 0
	}

	total := 0

	for idx := range node.routing.shards {
		snap := node.routing.shards[idx].ptr.Load()

		if snap == nil || snap.m == nil {
			continue
		}

		total += len(snap.m)
	}

	return total
}

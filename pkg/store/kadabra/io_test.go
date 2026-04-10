package kadabra

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
pinKadabraMeshForTrieFanout forces every distinct affinity to spawn its own trie
(ClusterThreshold 0) so we can exercise selectOrSpawnTrieScalar when the trie
count exceeds kernel.MaxNearestAffinityCandidates.
*/
func pinKadabraMeshForTrieFanout(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg

	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024

	/*
		Without a negative threshold, the batch-distance frame can report
		bestDist==0 before real kernel work finishes, which matches the lone
		trie under ClusterThreshold==0 and suppresses further spawns. -1 makes
		bestDist<=threshold false for non-negative distances so each distinct
		affinity gets its own trie under test.
	*/
	core.Cfg.Kadabra.ClusterThreshold = -1
}

func TestSelectOrSpawnTrieScalarPath(t *testing.T) {
	pinKadabraMeshForTrieFanout(t)

	Convey("many spawns hit scalar nearest-affinity path", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-io-scalar", queue)

		So(nErr, ShouldBeNil)

		// Scalar path runs only when len(tries) > MaxNearestAffinityCandidates.
		requiredSpawns := kernel.MaxNearestAffinityCandidates + 2

		for idx := range requiredSpawns {
			payload := fmt.Appendf(nil, "scalar-%d", idx)

			value, vErr := primitive.FirstSegment(primitive.NewValue(payload))

			So(vErr, ShouldBeNil)

			var aff [primitive.AffinityWords]uint64

			aff[0] = uint64(idx) + 1
			aff[1] = ^uint64(idx)
			aff[2] = uint64(idx) * 0xdeadbeef
			aff[3] = uint64(idx) << 17

			value.SetAffinityVector(aff)

			record := SequenceRecord{
				Value:     *value,
				Affinity:  value.AffinityVector(),
				Label:     fmt.Sprintf("L%d", idx),
				Publisher: node.ID,
				Key:       uint64(idx+1)<<32 | 0x7e710000 | uint64(idx),
			}

			So(node.Store(record), ShouldBeNil)

			_ = value.Close()

			queue.Drain()
		}

		ptr := node.tries.Load()

		So(ptr, ShouldNotBeNil)
		So(len(*ptr), ShouldBeGreaterThanOrEqualTo, requiredSpawns)
	})
}

func TestNodePredictManyTriesSelectsByAffinity(t *testing.T) {
	pinKadabraMeshForTrieFanout(t)

	Convey("Predict fans out when many tries exist", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-predict-fanout", queue)

		So(nErr, ShouldBeNil)

		for idx := range 10 {
			payload := fmt.Appendf(nil, "fan-%d", idx)

			value, vErr := primitive.FirstSegment(primitive.NewValue(payload))

			So(vErr, ShouldBeNil)

			var aff [primitive.AffinityWords]uint64

			aff[0] = uint64(idx) + 100
			aff[2] = uint64(idx) * 7

			value.SetAffinityVector(aff)

			record := SequenceRecord{
				Value:     *value,
				Affinity:  value.AffinityVector(),
				Label:     fmt.Sprintf("F%d", idx),
				Publisher: node.ID,
				Key:       uint64(idx+1)<<32 | 0xf00d0000 | uint64(idx),
			}

			So(node.Store(record), ShouldBeNil)

			_ = value.Close()

			queue.Drain()
		}

		queryPayload := []byte("fan-query")

		query, vErr := primitive.FirstSegment(primitive.NewValue(queryPayload))

		So(vErr, ShouldBeNil)

		defer query.Close()

		var qAff [primitive.AffinityWords]uint64

		qAff[0] = 105
		qAff[2] = 35

		query.SetAffinityVector(qAff)

		So(ensurePublishableAffinity(query, queryPayload), ShouldBeNil)

		pred, err := node.Predict(query)

		So(err, ShouldBeNil)
		So(pred, ShouldNotBeNil)
	})
}

func BenchmarkSelectOrSpawnTrieScalar(b *testing.B) {
	pinKadabraMeshForTrieFanout(b)

	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, err := NewNode(ctx, "kadabra-bench-io-scalar", queue)

	if err != nil {
		b.Fatal(err)
	}

	requiredSpawns := kernel.MaxNearestAffinityCandidates + 2

	for idx := range requiredSpawns {
		payload := fmt.Appendf(nil, "bench-scalar-%d", idx)

		value, err := primitive.FirstSegment(primitive.NewValue(payload))

		if err != nil {
			b.Fatal(err)
		}

		var aff [primitive.AffinityWords]uint64

		aff[0] = uint64(idx) + 0x1000
		aff[3] = ^uint64(idx * 3)

		value.SetAffinityVector(aff)

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     fmt.Sprintf("B%d", idx),
			Publisher: node.ID,
			Key:       uint64(idx+1)<<32 | 0xbe570000 | uint64(idx),
		}

		if err := node.Store(record); err != nil {
			_ = value.Close()

			b.Fatal(err)
		}

		_ = value.Close()

		queue.Drain()
	}

	aff := primitive.AffinityForNodeID(0xdeadbeef)

	b.ResetTimer()

	for b.Loop() {
		_ = node.selectOrSpawnTrie(aff)
	}
}

package kadabra

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestGossipDigests(t *testing.T) {
	t.Parallel()

	Convey("Digests is empty before any trie exists", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-gossip-empty", queue)

		So(nErr, ShouldBeNil)

		So(node.Gossip().Digests(), ShouldResemble, []Digest{})
	})

	Convey("after Store + Drain, Digests covers local tries", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-gossip-filled", queue)

		So(nErr, ShouldBeNil)

		value, vErr := primitive.NewValue([]byte("gossip-row"))

		So(vErr, ShouldBeNil)

		defer value.Close()

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     "g",
			Publisher: node.ID,
		}

		record.Key = record.Hash()

		So(node.Store(record), ShouldBeNil)

		queue.Drain()

		digs := node.Gossip().Digests()

		So(len(digs), ShouldEqual, 1)
		So(digs[0].Origin, ShouldEqual, (node.ID<<32)|1)
	})
}

func BenchmarkGossipDigests(b *testing.B) {
	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, err := NewNode(ctx, "kadabra-gossip-bench", queue)

	if err != nil {
		b.Fatal(err)
	}

	value, err := primitive.NewValue([]byte("gossip-bench"))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	record := SequenceRecord{
		Value:     *value,
		Affinity:  value.AffinityVector(),
		Label:     "",
		Publisher: node.ID,
	}

	record.Key = record.Hash()

	if err := node.Store(record); err != nil {
		b.Fatal(err)
	}

	queue.Drain()

	b.ResetTimer()

	for b.Loop() {
		_ = node.Gossip().Digests()
	}
}

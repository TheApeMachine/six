package kadabra

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
setupKadabraPrimitiveLayout pins Value layout while this test runs so parallel
packages that rewrite core.Cfg (see markovtrie.setupMarkovTrieValueConfig)
cannot strand NewValue / ComputeAffinityLSH with a truncated Words span,
which yields an all-zero affinity vector and breaks Publish.
*/
func setupKadabraPrimitiveLayout(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg

	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Words = 128
	core.Cfg.Value.Bytes = 1024
}

/*
ensurePublishableAffinity guarantees AffinityVector is non-zero for Kadabra
ingress. LSH is the primary path; n-gram context folding is the deterministic
fallback when the SimHash majority never sets a bit (possible on sparse slabs).
*/
func ensurePublishableAffinity(value *primitive.Value, payload []byte) error {
	if value == nil {
		return nil
	}

	if !primitive.AffinityVectorIsZero(value.AffinityVector()) {
		return nil
	}

	if err := value.ComputeAffinityFromContext(payload); err != nil {
		return err
	}

	if primitive.AffinityVectorIsZero(value.AffinityVector()) {
		return fmt.Errorf(
			"ensurePublishableAffinity: affinity still zero after FromContext",
		)
	}

	return nil
}

type stubRoutable struct{}

func (stub stubRoutable) AffinityVector() [primitive.AffinityWords]uint64 {
	return [primitive.AffinityWords]uint64{}
}

func (stub stubRoutable) String() string {
	return "stub-routable"
}

func TestNewNode(t *testing.T) {
	t.Parallel()

	Convey("nil context is rejected", t, func() {
		queue, qErr := pool.NewQueue(context.Background())

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, err := NewNode(nil, "kadabra-new-nil-ctx", queue) //nolint:staticcheck // Exercises nil-context guard in NewNode.

		So(node, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})

	Convey("empty id is rejected", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, err := NewNode(ctx, "   ", queue)

		So(node, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})

	Convey("valid construction returns node with queue and Field", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, err := NewNode(ctx, "kadabra-new-ok", queue)

		So(err, ShouldBeNil)
		So(node, ShouldNotBeNil)
		So(node.queue, ShouldEqual, queue)
		So(node.Field, ShouldNotBeNil)
	})
}

func TestNodeClose(t *testing.T) {
	t.Parallel()

	Convey("Close cancels context and returns err", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-close", queue)

		So(nErr, ShouldBeNil)

		So(node.Close(), ShouldBeNil)
	})
}

func TestNodeError(t *testing.T) {
	t.Parallel()

	Convey("fresh node has nil Error", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-err-state", queue)

		So(nErr, ShouldBeNil)

		So(node.Error(), ShouldBeNil)
	})
}

func TestNodeGossip(t *testing.T) {
	t.Parallel()

	Convey("Gossip returns non-nil layer", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-gossip", queue)

		So(nErr, ShouldBeNil)

		So(node.Gossip(), ShouldNotBeNil)
	})
}

func TestNodePredict(t *testing.T) {
	setupKadabraPrimitiveLayout(t)

	Convey("Predict rejects nil Routable", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-pred-nil", queue)

		So(nErr, ShouldBeNil)

		pred, err := node.Predict(nil)

		So(pred, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})

	Convey("Predict requires *primitive.Value", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-pred-type", queue)

		So(nErr, ShouldBeNil)

		pred, err := node.Predict(stubRoutable{})

		So(pred, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})

	Convey("Predict with Value runs without local tries", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-pred-value", queue)

		So(nErr, ShouldBeNil)

		payload := []byte("predict-token")

		value, vErr := primitive.FirstSegment(primitive.NewValue(payload))

		So(vErr, ShouldBeNil)

		defer value.Close()

		So(value.ComputeAffinityLSH(), ShouldBeNil)
		So(ensurePublishableAffinity(value, payload), ShouldBeNil)

		pred, err := node.Predict(value)

		So(err, ShouldBeNil)
		So(pred, ShouldNotBeNil)
	})

	Convey("Predict composes trie labels through the node classifier", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-pred-classify", queue)

		So(nErr, ShouldBeNil)

		payload := []byte("classification token")

		trainValue, vErr := primitive.FirstSegment(primitive.NewValue(payload))

		So(vErr, ShouldBeNil)

		defer trainValue.Close()

		So(trainValue.ComputeAffinityLSH(), ShouldBeNil)
		So(ensurePublishableAffinity(trainValue, payload), ShouldBeNil)
		So(node.Publish(trainValue, "alpha"), ShouldBeNil)

		queue.Drain()

		queryValue, qErr := primitive.FirstSegment(primitive.NewValue(payload))

		So(qErr, ShouldBeNil)

		defer queryValue.Close()

		So(queryValue.ComputeAffinityLSH(), ShouldBeNil)
		So(ensurePublishableAffinity(queryValue, payload), ShouldBeNil)

		pred, err := node.Predict(queryValue)

		So(err, ShouldBeNil)
		So(pred, ShouldNotBeNil)
		So(len(pred.Labels), ShouldBeGreaterThan, 0)
		So(pred.Label(), ShouldEqual, "alpha")
	})
}

func TestNodePublish(t *testing.T) {
	setupKadabraPrimitiveLayout(t)

	Convey("nil Value errors", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-pub-nil", queue)

		So(nErr, ShouldBeNil)

		So(node.Publish(nil, "x"), ShouldNotBeNil)
	})

	Convey("zero affinity is recomputed inside Publish", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-pub-aff", queue)

		So(nErr, ShouldBeNil)

		value, vErr := primitive.FirstSegment(primitive.NewValue([]byte("no-lsh")))

		So(vErr, ShouldBeNil)

		defer value.Close()

		value.SetAffinityVector([primitive.AffinityWords]uint64{})

		So(primitive.AffinityVectorIsZero(value.AffinityVector()), ShouldBeTrue)
		So(node.Publish(value, "lbl"), ShouldBeNil)
		So(primitive.AffinityVectorIsZero(value.AffinityVector()), ShouldBeFalse)
	})

	Convey("after ComputeAffinityLSH, Publish schedules ingest", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-pub-ok", queue)

		So(nErr, ShouldBeNil)

		payload := []byte("publish-ok")

		value, vErr := primitive.FirstSegment(primitive.NewValue(payload))

		So(vErr, ShouldBeNil)

		defer value.Close()

		So(value.ComputeAffinityLSH(), ShouldBeNil)
		So(ensurePublishableAffinity(value, payload), ShouldBeNil)

		So(node.Publish(value, "ok-label"), ShouldBeNil)

		queue.Drain()

		ptr := node.tries.Load()

		So(ptr, ShouldNotBeNil)
		So(len(*ptr), ShouldBeGreaterThan, 0)
	})
}

func BenchmarkNodePredict(b *testing.B) {
	setupKadabraPrimitiveLayout(b)

	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, err := NewNode(ctx, "kadabra-bench-predict", queue)

	if err != nil {
		b.Fatal(err)
	}

	payload := []byte("bench-predict")

	value, err := primitive.FirstSegment(primitive.NewValue(payload))

	if err != nil {
		b.Fatal(err)
	}

	defer value.Close()

	if err := value.ComputeAffinityLSH(); err != nil {
		b.Fatal(err)
	}

	if err := ensurePublishableAffinity(value, payload); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_, _ = node.Predict(value)
	}
}

func BenchmarkNodePublish(b *testing.B) {
	setupKadabraPrimitiveLayout(b)

	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, err := NewNode(ctx, "kadabra-bench-publish", queue)

	if err != nil {
		b.Fatal(err)
	}

	var counter int64

	b.ResetTimer()

	for b.Loop() {
		counter++

		payload := strconv.AppendInt(append([]byte("bench-pub-"), 'x'), counter, 10)

		value, err := primitive.FirstSegment(primitive.NewValue(payload))

		if err != nil {
			b.Fatal(err)
		}

		if err := value.ComputeAffinityLSH(); err != nil {
			_ = value.Close()

			b.Fatal(err)
		}

		if err := ensurePublishableAffinity(value, payload); err != nil {
			_ = value.Close()

			b.Fatal(err)
		}

		if err := node.Publish(value, ""); err != nil {
			_ = value.Close()

			b.Fatal(err)
		}

		queue.Drain()

		_ = value.Close()
	}
}

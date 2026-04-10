package markovtrie

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestStoreWalk(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Walk visits every reachable node", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		first, vErr := primitive.FirstSegment(primitive.NewValue([]byte("w1")))

		So(vErr, ShouldBeNil)

		defer first.Close()

		second, vErr := primitive.FirstSegment(primitive.NewValue([]byte("w2")))

		So(vErr, ShouldBeNil)

		defer second.Close()

		So(store.Load(*first), ShouldBeNil)
		So(store.Load(*second), ShouldBeNil)

		seen := 0

		store.Walk(store.root, func(node *Node) {
			seen++
		})

		So(seen, ShouldBeGreaterThanOrEqualTo, 2)
	})
}

func TestStoreWalkPath(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("WalkPath stops at deepest existing prefix", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		tok, vErr := primitive.FirstSegment(primitive.NewValue([]byte("exists")))

		So(vErr, ShouldBeNil)

		defer tok.Close()

		So(store.Load(*tok), ShouldBeNil)

		token := trieEdgeKey(*tok)

		steps := 0

		leaf := store.WalkPath([]string{token, "missing-branch"}, func(node *Node) {
			steps++
		})

		So(steps, ShouldEqual, 2)
		So(leaf.Child("missing-branch"), ShouldBeNil)
	})
}

func BenchmarkStoreWalk(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	store, err := NewStore(context.Background(), primitive.Affinity{})

	if err != nil {
		b.Fatal(err)
	}

	for idx := range 6 {
		payload := append([]byte("walk-node-"), byte('a'+idx))

		tok, err := primitive.FirstSegment(primitive.NewValue(payload))

		if err != nil {
			b.Fatal(err)
		}

		if err := store.Load(*tok); err != nil {
			_ = tok.Close()

			b.Fatal(err)
		}

		_ = tok.Close()
	}

	b.ResetTimer()

	for b.Loop() {
		store.Walk(store.root, func(node *Node) {
			_ = node
		})
	}
}

func BenchmarkStoreWalkPath(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	store, err := NewStore(context.Background(), primitive.Affinity{})

	if err != nil {
		b.Fatal(err)
	}

	tok, err := primitive.FirstSegment(primitive.NewValue([]byte("walk-bench")))

	if err != nil {
		b.Fatal(err)
	}

	defer tok.Close()

	if err := store.Load(*tok); err != nil {
		b.Fatal(err)
	}

	token := trieEdgeKey(*tok)

	b.ResetTimer()

	for b.Loop() {
		store.WalkPath([]string{token}, func(node *Node) {
			_ = node
		})
	}
}

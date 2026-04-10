package markovtrie

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestStoreGenerate(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Generate on nil store returns empty", t, func() {
		var store *Store

		out, err := store.Generate("ctx", "", 1, 0)

		So(err, ShouldBeNil)
		So(out, ShouldEqual, "")
	})

	Convey("when context has no tokens, Generate walks from root", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		tok, vErr := primitive.FirstSegment(primitive.NewValue([]byte("root-child")))

		So(vErr, ShouldBeNil)

		defer tok.Close()

		So(store.Load(*tok), ShouldBeNil)

		out, err := store.Generate("   \t\n", "", 1, 8)

		So(err, ShouldBeNil)
		So(len(out), ShouldBeGreaterThan, 0)
	})

	Convey("Generate emits continuation blob after branching", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		parent, vErr := primitive.FirstSegment(primitive.NewValue([]byte("gen-p")))

		So(vErr, ShouldBeNil)

		defer parent.Close()

		child, vErr := primitive.FirstSegment(primitive.NewValue([]byte("gen-c")))

		So(vErr, ShouldBeNil)

		defer child.Close()

		So(store.Load(*parent), ShouldBeNil)
		So(store.Load(*child), ShouldBeNil)

		out, err := store.Generate("gen-p", "", 1, 8)

		So(err, ShouldBeNil)
		So(len(out), ShouldBeGreaterThan, 0)
	})
}

func BenchmarkStoreGenerate(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	store, err := NewStore(context.Background(), primitive.Affinity{})

	if err != nil {
		b.Fatal(err)
	}

	parent, err := primitive.FirstSegment(primitive.NewValue([]byte("gb-p")))

	if err != nil {
		b.Fatal(err)
	}

	defer parent.Close()

	child, err := primitive.FirstSegment(primitive.NewValue([]byte("gb-c")))

	if err != nil {
		b.Fatal(err)
	}

	defer child.Close()

	if err := store.Load(*parent); err != nil {
		b.Fatal(err)
	}

	if err := store.Load(*child); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_, _ = store.Generate("gb-p", "", 1, 8)
	}
}

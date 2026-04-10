package markovtrie

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestNewNode(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("NewNode wires ID from Value and empty children map", t, func() {
		value, err := primitive.FirstSegment(primitive.NewValue([]byte("node-id")))

		So(err, ShouldBeNil)

		defer value.Close()

		node := NewNode(*value)

		So(node.ID, ShouldEqual, value.ID())
		So(node.Children(), ShouldNotBeNil)
		So(len(node.Children()), ShouldEqual, 0)
	})
}

func TestNodeChild(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Child misses when map empty", t, func() {
		value, err := primitive.FirstSegment(primitive.NewValue([]byte("solo")))

		So(err, ShouldBeNil)

		defer value.Close()

		node := NewNode(*value)

		So(node.Child("missing"), ShouldBeNil)
	})
}

func TestNodeStoreChild(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("storeChild inserts under trieEdgeKey token", t, func() {
		rootVal, err := primitive.FirstSegment(primitive.NewValue([]byte("root")))

		So(err, ShouldBeNil)

		defer rootVal.Close()

		childVal, err := primitive.FirstSegment(primitive.NewValue([]byte("leaf")))

		So(err, ShouldBeNil)

		defer childVal.Close()

		root := NewNode(*rootVal)
		child := NewNode(*childVal)

		root.storeChild(*childVal, child)

		key := trieEdgeKey(*childVal)

		So(root.Child(key), ShouldEqual, child)
		So(child.parent.Load(), ShouldEqual, root)
		So(child.TransitionMotor().IsZero(), ShouldBeFalse)
	})
}

func BenchmarkNodeStoreChild(b *testing.B) {
	setupMarkovTrieValueConfig(b)

	rootVal, err := primitive.FirstSegment(primitive.NewValue([]byte("r")))

	if err != nil {
		b.Fatal(err)
	}

	defer rootVal.Close()

	root := NewNode(*rootVal)

	childVal, err := primitive.FirstSegment(primitive.NewValue([]byte("c")))

	if err != nil {
		b.Fatal(err)
	}

	defer childVal.Close()

	child := NewNode(*childVal)

	b.ResetTimer()

	for b.Loop() {
		root.storeChild(*childVal, child)
	}
}

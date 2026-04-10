package markovtrie

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestStoreSnapshotVizGraph(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	Convey("SnapshotVizGraph empty store yields nothing", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		_, nodes, edges, truncated := store.SnapshotVizGraph(10)

		So(nodes, ShouldBeNil)
		So(edges, ShouldBeNil)
		So(truncated, ShouldBeFalse)
	})

	Convey("SnapshotVizGraph matches Load branch structure", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		first, vErr := primitive.FirstSegment(primitive.NewValue([]byte("aa")))

		So(vErr, ShouldBeNil)

		defer first.Close()

		second, vErr := primitive.FirstSegment(primitive.NewValue([]byte("bb")))

		So(vErr, ShouldBeNil)

		defer second.Close()

		So(store.Load(*first), ShouldBeNil)
		So(store.Load(*second), ShouldBeNil)

		rootVid, nodes, edges, truncated := store.SnapshotVizGraph(32)

		So(truncated, ShouldBeFalse)
		So(len(nodes), ShouldEqual, 3)
		So(rootVid, ShouldEqual, 0)
		So(nodes[0].Vid, ShouldEqual, 0)

		So(len(edges), ShouldEqual, 2)

		edgeTok := []string{edges[0].Token, edges[1].Token}

		So(edgeTok, ShouldContain, trieEdgeKey(*first))
		So(edgeTok, ShouldContain, trieEdgeKey(*second))
	})

	Convey("SnapshotVizGraph sets truncated when cap cuts frontier", t, func() {
		store, err := NewStore(context.Background(), primitive.Affinity{})

		So(err, ShouldBeNil)

		var values []*primitive.Value

		defer func() {
			for _, v := range values {
				v.Close()
			}
		}()

		for i := range 5 {
			v, vErr := primitive.FirstSegment(primitive.NewValue([]byte{byte('a' + i)}))

			So(vErr, ShouldBeNil)

			values = append(values, v)
			So(store.Load(*v, "branch"), ShouldBeNil)
		}

		/*
		   root → five children (labels reset lastLeaf each Load). With cap 2,
		   only root and the first breadth-first child are materialized; the
		   remaining siblings stay queued so Truncated is true.
		*/
		_, _, _, truncated := store.SnapshotVizGraph(2)

		So(truncated, ShouldBeTrue)
	})
}

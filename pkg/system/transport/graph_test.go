package transport

import (
	"bytes"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

/*
memRW is a small in-memory ReadWriteCloser for graph tests.
*/
type memRW struct {
	bytes.Buffer
}

func (mem *memRW) Close() error {
	return nil
}

func TestGraph(t *testing.T) {
	Convey("Given a graph without registry", t, func() {
		graph := NewGraph()

		Convey("Read should error", func() {
			buf := make([]byte, 8)
			n, err := graph.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldNotBeNil)
		})

		Convey("Write should error", func() {
			n, err := graph.Write([]byte{1})
			So(n, ShouldEqual, 0)
			So(err, ShouldNotBeNil)
		})

		Convey("Close should succeed with no registry", func() {
			So(graph.Close(), ShouldBeNil)
		})

		Convey("GetEdges should return empty slice", func() {
			So(graph.GetEdges("missing"), ShouldResemble, []string{})
		})
	})

	Convey("Given a graph with registry and edge flow", t, func() {
		reg := new(memRW)
		src := new(memRW)
		dst := new(memRW)

		_, err := src.WriteString("edge")
		So(err, ShouldBeNil)

		graph := NewGraph(
			WithRegistry(reg),
			WithNode(&Node{ID: "a", Component: src}),
			WithNode(&Node{ID: "b", Component: dst}),
			WithEdge(&Edge{From: "a", To: "b"}),
		)

		Convey("GetEdges should list destinations", func() {
			So(graph.GetEdges("a"), ShouldResemble, []string{"b"})
		})

		Convey("Write then Read should process edge and return registry data", func() {
			wn, werr := graph.Write([]byte("reg"))
			So(werr, ShouldBeNil)
			So(wn, ShouldEqual, 3)

			// first Read runs edge copies (a -> b) then reads registry
			out := make([]byte, 32)
			rn, rerr := graph.Read(out)
			So(rerr, ShouldBeNil)
			So(rn, ShouldEqual, 3)
			So(string(out[:rn]), ShouldEqual, "reg")

			So(dst.String(), ShouldEqual, "edge")
		})

		Convey("Close should close registry", func() {
			So(graph.Close(), ShouldBeNil)
		})
	})

	Convey("Given a graph with an edge whose source node is missing", t, func() {
		reg := new(memRW)
		dst := new(memRW)

		graph := NewGraph(
			WithRegistry(reg),
			WithNode(&Node{ID: "b", Component: dst}),
			WithEdge(&Edge{From: "a", To: "b"}),
		)

		Convey("Read should return a descriptive source-node error", func() {
			_, err := graph.Write([]byte("reg"))
			So(err, ShouldBeNil)

			out := make([]byte, 32)
			n, err := graph.Read(out)
			So(n, ShouldEqual, 0)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "missing source node")
			So(err.Error(), ShouldContainSubstring, "a")
		})
	})

	Convey("Given a graph with an edge whose target node is missing", t, func() {
		reg := new(memRW)
		src := new(memRW)

		_, err := src.WriteString("edge")
		So(err, ShouldBeNil)

		graph := NewGraph(
			WithRegistry(reg),
			WithNode(&Node{ID: "a", Component: src}),
			WithEdge(&Edge{From: "a", To: "b"}),
		)

		Convey("Read should return a descriptive target-node error", func() {
			_, err := graph.Write([]byte("reg"))
			So(err, ShouldBeNil)

			out := make([]byte, 32)
			n, err := graph.Read(out)
			So(n, ShouldEqual, 0)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "missing target node")
			So(err.Error(), ShouldContainSubstring, "b")
		})
	})
}

func BenchmarkGraphWriteRead(b *testing.B) {
	reg := new(memRW)
	src := new(memRW)
	dst := new(memRW)

	graph := NewGraph(
		WithRegistry(reg),
		WithNode(&Node{ID: "a", Component: src}),
		WithNode(&Node{ID: "b", Component: dst}),
		WithEdge(&Edge{From: "a", To: "b"}),
	)

	payload := []byte("payload")
	readBuf := make([]byte, 64)
	b.ReportAllocs()

	for b.Loop() {
		reg.Reset()
		src.Reset()
		dst.Reset()
		_, _ = src.WriteString("e")

		_, _ = graph.Write(payload)
		_, _ = graph.Read(readBuf)
	}
}

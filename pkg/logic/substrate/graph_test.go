package substrate

import (
	"context"
	"runtime"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/pool"
)

func testGraph(t *testing.T) *GraphServer {
	ctx := context.Background()

	workerPool := pool.New(ctx, 1, max(2, runtime.NumCPU()), &pool.Config{})
	t.Cleanup(func() { workerPool.Close() })

	return NewGraphServer(
		GraphWithContext(ctx),
		GraphWithWorkerPool(workerPool),
	)
}

func TestGraphServer(t *testing.T) {
	Convey("Given a configured GraphServer", t, func() {
		graph := testGraph(t)
		So(graph, ShouldNotBeNil)
		So(graph.state.Err(), ShouldBeNil)
	})
}

func TestWrite(t *testing.T) {
	Convey("Given Graph.write with a morton key", t, func() {
		graph := testGraph(t)
		ctx := context.Background()
		client := Graph_ServerToClient(graph)

		err := client.Write(ctx, func(p Graph_write_Params) error {
			p.SetKey(0)

			return nil
		})

		So(err, ShouldBeNil)
		So(client.WaitStreaming(), ShouldBeNil)
		So(graph.Load(), ShouldEqual, 1)
	})
}

func TestGraphLoad(t *testing.T) {
	Convey("Load should reflect row count after writes at position zero", t, func() {
		graph := testGraph(t)
		ctx := context.Background()
		client := Graph_ServerToClient(graph)

		So(graph.Load(), ShouldEqual, 0)

		writeErr := client.Write(ctx, func(p Graph_write_Params) error {
			p.SetKey(0)

			return nil
		})

		So(writeErr, ShouldBeNil)
		So(client.WaitStreaming(), ShouldBeNil)
		So(graph.Load(), ShouldEqual, 1)
	})
}

func TestDone(t *testing.T) {
	Convey("Graph.done should aggregate chunks into signals", t, func() {
		graph := testGraph(t)
		ctx := context.Background()
		client := Graph_ServerToClient(graph)

		writeErr := client.Write(ctx, func(p Graph_write_Params) error {
			p.SetKey(0)

			return nil
		})

		So(writeErr, ShouldBeNil)
		So(client.WaitStreaming(), ShouldBeNil)

		future, release := client.Done(ctx, nil)
		defer release()

		_, err := future.Struct()
		So(err, ShouldBeNil)
		So(len(graph.signals), ShouldEqual, 1)
	})
}

func TestRecursiveFold(t *testing.T) {
	Convey("RecursiveFold should terminate on small inputs", t, func() {
		graph := testGraph(t)

		left := primitive.BaseValue(3)
		right := primitive.BaseValue(5)

		out := graph.RecursiveFold([]primitive.Value{left, right})
		So(out, ShouldNotBeNil)
		So(len(out), ShouldEqual, 1)
		So(len(out[0]), ShouldEqual, 2)
	})
}

func TestExactContinuation(t *testing.T) {
	Convey("Given stored graph rows with an exact prompt prefix", t, func() {
		graph := testGraph(t)
		ctx := context.Background()
		client := Graph_ServerToClient(graph)
		coder := data.NewMortonCoder()

		writeKeys := func(text string) {
			for index, symbol := range []byte(text) {
				writeErr := client.Write(ctx, func(p Graph_write_Params) error {
					p.SetKey(coder.Pack(uint32(index), symbol))
					return nil
				})

				So(writeErr, ShouldBeNil)
				So(client.WaitStreaming(), ShouldBeNil)
			}
		}

		writeKeys("Roy is in the Kitchen")

		prompt := make([]primitive.Value, 0, len("Roy is in the "))
		for index, symbol := range []byte("Roy is in the ") {
			prompt = append(
				prompt,
				primitive.SeedObservable(
					symbol,
					primitive.BaseValue(symbol).RollLeft(index),
				),
			)
		}

		Convey("It should return only the unmatched continuation suffix", func() {
			remainder := graph.ExactContinuation(prompt)
			out := make([]byte, 0, len(remainder))

			for _, value := range remainder {
				symbol, ok := primitive.InferLexicalSeed(value)
				if ok {
					out = append(out, symbol)
				}
			}

			So(out, ShouldResemble, []byte("Kitchen"))
		})
	})
}

func BenchmarkGraphWriteDone(b *testing.B) {
	ctx := context.Background()

	workerPool := pool.New(ctx, 1, max(2, runtime.NumCPU()), &pool.Config{})
	defer workerPool.Close()

	graph := NewGraphServer(
		GraphWithContext(ctx),
		GraphWithWorkerPool(workerPool),
	)

	client := Graph_ServerToClient(graph)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = client.Write(ctx, func(p Graph_write_Params) error {
			p.SetKey(0)

			return nil
		})

		_ = client.WaitStreaming()

		future, release := client.Done(ctx, nil)
		_, _ = future.Struct()
		release()
	}
}

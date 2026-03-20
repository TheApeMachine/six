package remote

import (
	"context"
	"io"
	"runtime"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/dmt"
	"github.com/theapemachine/six/pkg/system/pool"
)

type testHarness struct {
	netNode *dmt.NetworkNode
	forest  *dmt.Forest
	pool    *pool.Pool
	addr    string
	ctx     context.Context
	cancel  context.CancelFunc
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	workerPool := pool.New(ctx, 2, max(2, runtime.NumCPU()), &pool.Config{})

	forest, forestErr := dmt.NewForest(dmt.ForestConfig{
		Pool: workerPool,
	})

	if forestErr != nil {
		workerPool.Close()
		cancel()
		t.Fatalf("failed to create forest: %v", forestErr)
	}

	netNode, netErr := dmt.NewNetworkNode(dmt.NetworkConfig{
		ListenAddr: "127.0.0.1:0",
		NodeID:     "test-node",
	}, forest)

	if netErr != nil {
		forest.Close()
		workerPool.Close()
		cancel()
		t.Fatalf("failed to create network node: %v", netErr)
	}

	return &testHarness{
		netNode: netNode,
		forest:  forest,
		pool:    workerPool,
		addr:    netNode.ListenAddr(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (harness *testHarness) close() {
	harness.netNode.Close()
	harness.forest.Close()
	harness.pool.Close()
	harness.cancel()
}

func TestNodeAvailableBeforeConnect(t *testing.T) {
	Convey("Given a Node that has not connected yet", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(
			NodeWithContext(ctx),
			NodeWithAddress("127.0.0.1:1"),
		)

		Convey("It should report Available zero", func() {
			n, err := node.Available()
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestNodeConnectAndWrite(t *testing.T) {
	harness := newTestHarness(t)
	defer harness.close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := NewNode(
		NodeWithContext(ctx),
		NodeWithAddress(harness.addr),
	)
	defer node.Close()

	key := []byte("test-key-alpha")
	n, writeErr := node.Write(key)

	if writeErr != nil {
		t.Fatalf("Write failed: %v", writeErr)
	}

	Convey("Given a successful write to a remote dmt node", t, func() {
		Convey("It should return the correct byte count and mark available", func() {
			So(n, ShouldEqual, len(key))

			avail, availErr := node.Available()
			So(availErr, ShouldBeNil)
			So(avail, ShouldEqual, 1)
		})

		Convey("It should store the key in the remote forest", func() {
			time.Sleep(50 * time.Millisecond)
			_, found := harness.forest.Get(key)
			So(found, ShouldBeTrue)
		})
	})
}

func TestNodeWriteMultipleKeys(t *testing.T) {
	harness := newTestHarness(t)
	defer harness.close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := NewNode(
		NodeWithContext(ctx),
		NodeWithAddress(harness.addr),
	)
	defer node.Close()

	keys := [][]byte{
		[]byte("key-one"),
		[]byte("key-two"),
		[]byte("key-three"),
	}

	for _, key := range keys {
		n, err := node.Write(key)

		if err != nil {
			t.Fatalf("Write(%s) failed: %v", key, err)
		}

		if n != len(key) {
			t.Fatalf("Write(%s) returned %d, want %d", key, n, len(key))
		}
	}

	Convey("Given multiple keys written to a remote node", t, func() {
		Convey("All keys should exist in the remote forest", func() {
			time.Sleep(50 * time.Millisecond)

			for _, key := range keys {
				_, found := harness.forest.Get(key)
				So(found, ShouldBeTrue)
			}
		})
	})
}

func TestNodeReadEmptyBuffer(t *testing.T) {
	harness := newTestHarness(t)
	defer harness.close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := NewNode(
		NodeWithContext(ctx),
		NodeWithAddress(harness.addr),
	)
	defer node.Close()

	_, writeErr := node.Write([]byte("trigger-connect"))

	if writeErr != nil {
		t.Fatalf("Write failed: %v", writeErr)
	}

	Convey("Given a connected node with no sync data", t, func() {
		Convey("Read should return EOF", func() {
			buf := make([]byte, 64)
			_, readErr := node.Read(buf)
			So(readErr, ShouldEqual, io.EOF)
		})
	})
}

func TestNodeClose(t *testing.T) {
	harness := newTestHarness(t)
	defer harness.close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := NewNode(
		NodeWithContext(ctx),
		NodeWithAddress(harness.addr),
	)

	_, writeErr := node.Write([]byte("connect"))

	if writeErr != nil {
		t.Fatalf("Write failed: %v", writeErr)
	}

	Convey("Given a connected node that is closed", t, func() {
		closeErr := node.Close()
		So(closeErr, ShouldBeNil)

		Convey("It should report Available zero", func() {
			n, _ := node.Available()
			So(n, ShouldEqual, 0)
		})
	})
}

func TestNodeConnectToUnreachable(t *testing.T) {
	Convey("Given a node targeting an unreachable address", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(
			NodeWithContext(ctx),
			NodeWithAddress("127.0.0.1:1"),
		)
		defer node.Close()

		Convey("Write should return a connection error", func() {
			_, writeErr := node.Write([]byte("nope"))
			So(writeErr, ShouldNotBeNil)
		})
	})
}

func BenchmarkNodeWrite(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerPool := pool.New(ctx, 2, max(2, runtime.NumCPU()), &pool.Config{})
	defer workerPool.Close()

	forest, err := dmt.NewForest(dmt.ForestConfig{Pool: workerPool})

	if err != nil {
		b.Fatalf("forest: %v", err)
	}

	defer forest.Close()

	netNode, netErr := dmt.NewNetworkNode(dmt.NetworkConfig{
		ListenAddr: "127.0.0.1:0",
		NodeID:     "bench-node",
	}, forest)

	if netErr != nil {
		b.Fatalf("network node: %v", netErr)
	}

	defer netNode.Close()

	time.Sleep(50 * time.Millisecond)
	addr := netNode.ListenAddr()

	node := NewNode(
		NodeWithContext(ctx),
		NodeWithAddress(addr),
	)
	defer node.Close()

	key := []byte("bench-key-data")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		node.Write(key)
	}
}

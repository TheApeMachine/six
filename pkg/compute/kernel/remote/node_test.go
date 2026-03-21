package remote

import (
	"context"
	"errors"
	"io"
	"net"
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

type waitHelper interface {
	Helper()
	Fatalf(format string, args ...any)
}

/*
waitListenerTCP blocks until addr accepts a TCP connection or the deadline passes.
*/
func waitListenerTCP(t waitHelper, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)

		if err == nil {
			conn.Close()

			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("tcp %s did not accept within timeout", addr)
}

/*
waitForestKey polls forest.Get until the key appears or timeout.
*/
func waitForestKey(t *testing.T, forest *dmt.Forest, key []byte, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, ok := forest.Get(key); ok {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("forest missing key after %v", timeout)
}

func TestNodeAvailableBeforeConnect(t *testing.T) {
	Convey("Given a Node that has not connected yet", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(
			NodeWithContext(ctx),
			NodeWithAddress("127.0.0.1:1"),
		)

		Convey("It should report Available for routing before the first dial", func() {
			n, err := node.Available()
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 1)
		})
	})
}

func TestNodeConnectAndWrite(t *testing.T) {
	Convey("Given a successful write to a remote dmt node", t, func() {
		harness := newTestHarness(t)
		defer harness.close()

		waitListenerTCP(t, harness.addr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(
			NodeWithContext(ctx),
			NodeWithAddress(harness.addr),
		)
		defer node.Close()

		key := []byte("test-key-alpha")
		n, writeErr := node.WriteSync(key)
		So(writeErr, ShouldBeNil)
		So(n, ShouldEqual, len(key))

		avail, availErr := node.Available()
		So(availErr, ShouldBeNil)
		So(avail, ShouldEqual, 1)

		waitForestKey(t, harness.forest, key, 2*time.Second)
		_, found := harness.forest.Get(key)
		So(found, ShouldBeTrue)
	})
}

func TestNodePipelinedWrite(t *testing.T) {
	Convey("Given multiple pipelined writes to a remote node", t, func() {
		harness := newTestHarness(t)
		defer harness.close()

		waitListenerTCP(t, harness.addr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(
			NodeWithContext(ctx),
			NodeWithAddress(harness.addr),
		)
		defer node.Close()

		keys := [][]byte{
			[]byte("pipe-one"),
			[]byte("pipe-two"),
			[]byte("pipe-three"),
		}

		for _, key := range keys {
			n, err := node.Write(key)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(key))
		}

		for _, key := range keys {
			waitForestKey(t, harness.forest, key, 2*time.Second)
		}

		for _, key := range keys {
			_, found := harness.forest.Get(key)
			So(found, ShouldBeTrue)
		}

		So(node.LastError(), ShouldBeNil)
	})
}

func TestNodeLastErrorPreservesFirstFailure(t *testing.T) {
	Convey("Given a node that records multiple terminal errors", t, func() {
		node := NewNode()
		firstErr := errors.New("first failure")
		secondErr := errors.New("second failure")

		node.storeErr(firstErr)
		node.storeErr(secondErr)

		Convey("It should preserve the first error for callers", func() {
			So(node.LastError(), ShouldEqual, firstErr)
		})
	})
}

func TestNodeWriteAfterClose(t *testing.T) {
	Convey("Given a closed node", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(NodeWithContext(ctx))
		So(node.Close(), ShouldBeNil)

		Convey("It should reject further writes with ErrClosedPipe", func() {
			n, err := node.Write([]byte("late"))
			So(err, ShouldEqual, io.ErrClosedPipe)
			So(n, ShouldEqual, 0)
		})
	})
}

func TestNodeWriteMultipleKeys(t *testing.T) {
	Convey("Given multiple keys written to a remote node", t, func() {
		harness := newTestHarness(t)
		defer harness.close()

		waitListenerTCP(t, harness.addr)

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
			n, err := node.WriteSync(key)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len(key))
		}

		for _, key := range keys {
			waitForestKey(t, harness.forest, key, 2*time.Second)
			_, found := harness.forest.Get(key)
			So(found, ShouldBeTrue)
		}
	})
}

func TestNodeReadEmptyBuffer(t *testing.T) {
	Convey("Given a connected node with no sync data", t, func() {
		harness := newTestHarness(t)
		defer harness.close()

		waitListenerTCP(t, harness.addr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(
			NodeWithContext(ctx),
			NodeWithAddress(harness.addr),
		)
		defer node.Close()

		_, writeErr := node.WriteSync([]byte("trigger-connect"))
		So(writeErr, ShouldBeNil)

		buf := make([]byte, 64)
		_, readErr := node.Read(buf)
		So(readErr, ShouldEqual, io.EOF)
	})
}

func TestNodeClose(t *testing.T) {
	Convey("Given a connected node that is closed", t, func() {
		harness := newTestHarness(t)
		defer harness.close()

		waitListenerTCP(t, harness.addr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		node := NewNode(
			NodeWithContext(ctx),
			NodeWithAddress(harness.addr),
		)

		_, writeErr := node.WriteSync([]byte("connect"))
		So(writeErr, ShouldBeNil)

		closeErr := node.Close()
		So(closeErr, ShouldBeNil)

		n, availErr := node.Available()
		So(availErr, ShouldBeNil)
		So(n, ShouldEqual, 0)
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

	addr := netNode.ListenAddr()
	waitListenerTCP(b, addr)

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

func BenchmarkNodeWriteSync(b *testing.B) {
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
		NodeID:     "bench-node-sync",
	}, forest)

	if netErr != nil {
		b.Fatalf("network node: %v", netErr)
	}

	defer netNode.Close()

	addr := netNode.ListenAddr()
	waitListenerTCP(b, addr)

	node := NewNode(
		NodeWithContext(ctx),
		NodeWithAddress(addr),
	)
	defer node.Close()

	key := []byte("bench-key-sync")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		node.WriteSync(key)
	}
}

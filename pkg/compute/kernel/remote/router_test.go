package remote

import (
	"context"
	"io"
	"runtime"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/dmt"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/pool"
)

func TestRouterNoPeers(t *testing.T) {
	Convey("Given a router with no peers", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		router := NewRouter(RouterWithContext(ctx))
		defer router.Close()

		Convey("It should report Available zero and reject io", func() {
			n, err := router.Available()
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 0)

			_, writeErr := router.Write([]byte("key"))
			So(writeErr, ShouldEqual, io.ErrClosedPipe)

			buf := make([]byte, 32)
			_, readErr := router.Read(buf)
			So(readErr, ShouldEqual, io.EOF)
		})
	})
}

func TestRouterAddPeerAndWrite(t *testing.T) {
	harness := newTestHarness(t)
	defer harness.close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := NewRouter(RouterWithContext(ctx))
	defer router.Close()

	if err := router.AddPeer(harness.addr); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	key := []byte("router-test-key")
	n, writeErr := router.Write(key)

	if writeErr != nil {
		t.Fatalf("Write: %v", writeErr)
	}

	Convey("Given a key written through the router", t, func() {
		Convey("It should succeed and reach the remote forest", func() {
			So(n, ShouldEqual, len(key))

			_, found := harness.forest.Get(key)
			So(found, ShouldBeTrue)
		})
	})
}

func TestRouterMultiplePeers(t *testing.T) {
	h1 := newTestHarness(t)
	defer h1.close()

	h2 := newTestHarness(t)
	defer h2.close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := NewRouter(RouterWithContext(ctx))
	defer router.Close()

	router.AddPeer(h1.addr)
	router.AddPeer(h2.addr)

	key := []byte("multi-peer-key")
	n, writeErr := router.Write(key)

	if writeErr != nil {
		t.Fatalf("Write: %v", writeErr)
	}

	Convey("Given a router with two live peers", t, func() {
		Convey("It should track both and route a write to one", func() {
			nodes := router.Nodes()
			So(len(nodes), ShouldEqual, 2)
			So(n, ShouldEqual, len(key))

			_, found1 := h1.forest.Get(key)
			_, found2 := h2.forest.Get(key)
			So(found1 || found2, ShouldBeTrue)
		})
	})
}

func TestRouterWithCluster(t *testing.T) {
	Convey("Given a router with a cluster reference", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		clusterRouter := cluster.NewRouter(
			cluster.RouterWithContext(ctx),
		)

		router := NewRouter(
			RouterWithContext(ctx),
			RouterWithCluster(clusterRouter),
		)
		defer router.Close()

		Convey("It should hold the cluster reference", func() {
			So(router.cluster, ShouldNotBeNil)
		})
	})
}

func TestRouterClose(t *testing.T) {
	harness := newTestHarness(t)
	defer harness.close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := NewRouter(RouterWithContext(ctx))
	router.AddPeer(harness.addr)

	_, writeErr := router.Write([]byte("trigger-connect"))

	if writeErr != nil {
		t.Fatalf("Write: %v", writeErr)
	}

	Convey("Given a connected router that is closed", t, func() {
		closeErr := router.Close()
		So(closeErr, ShouldBeNil)

		Convey("It should report zero available and empty nodes", func() {
			n, _ := router.Available()
			So(n, ShouldEqual, 0)
			So(len(router.Nodes()), ShouldEqual, 0)
		})
	})
}

func BenchmarkRouterWrite(b *testing.B) {
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
		NodeID:     "bench-router",
	}, forest)

	if netErr != nil {
		b.Fatalf("network node: %v", netErr)
	}

	defer netNode.Close()

	time.Sleep(50 * time.Millisecond)
	addr := netNode.ListenAddr()

	router := NewRouter(RouterWithContext(ctx))
	defer router.Close()

	router.AddPeer(addr)

	key := []byte("bench-router-key")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.Write(key)
	}
}

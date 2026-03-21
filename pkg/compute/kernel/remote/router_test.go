package remote

import (
	"context"
	"runtime"
	"testing"

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
			So(writeErr, ShouldEqual, ErrNoPeers)

			buf := make([]byte, 32)
			_, readErr := router.Read(buf)
			So(readErr, ShouldEqual, ErrNoPeers)
		})
	})
}

func TestRouterAddPeerAndWrite(t *testing.T) {
	Convey("Given a router peer and a remote forest", t, func() {
		harness := newTestHarness(t)
		defer harness.close()

		waitListenerTCP(t, harness.addr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		router := NewRouter(RouterWithContext(ctx))
		defer router.Close()

		addErr := router.AddPeer(harness.addr)
		So(addErr, ShouldBeNil)

		key := []byte("router-test-key")
		n, writeErr := router.WriteSync(key)
		So(writeErr, ShouldBeNil)

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

	waitListenerTCP(t, h1.addr)
	waitListenerTCP(t, h2.addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := NewRouter(RouterWithContext(ctx))
	defer router.Close()

	router.AddPeer(h1.addr)
	router.AddPeer(h2.addr)

	key := []byte("multi-peer-key")
	n, writeErr := router.WriteSync(key)

	if writeErr != nil {
		t.Fatalf("WriteSync: %v", writeErr)
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

	waitListenerTCP(t, harness.addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := NewRouter(RouterWithContext(ctx))
	router.AddPeer(harness.addr)

	_, writeErr := router.WriteSync([]byte("trigger-connect"))

	if writeErr != nil {
		t.Fatalf("WriteSync: %v", writeErr)
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

func setupBenchmarkRouter(b *testing.B, nodeID string) (*Router, func()) {
	b.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	workerPool := pool.New(ctx, 2, max(2, runtime.NumCPU()), &pool.Config{})

	forest, err := dmt.NewForest(dmt.ForestConfig{Pool: workerPool})
	if err != nil {
		workerPool.Close()
		cancel()
		b.Fatalf("forest: %v", err)
	}

	netNode, netErr := dmt.NewNetworkNode(dmt.NetworkConfig{
		ListenAddr: "127.0.0.1:0",
		NodeID:     nodeID,
	}, forest)
	if netErr != nil {
		forest.Close()
		workerPool.Close()
		cancel()
		b.Fatalf("network node: %v", netErr)
	}

	addr := netNode.ListenAddr()
	waitListenerTCP(b, addr)

	router := NewRouter(RouterWithContext(ctx))
	if addErr := router.AddPeer(addr); addErr != nil {
		router.Close()
		netNode.Close()
		forest.Close()
		workerPool.Close()
		cancel()
		b.Fatalf("add peer: %v", addErr)
	}

	cleanup := func() {
		_ = router.Close()
		_ = netNode.Close()
		_ = forest.Close()
		workerPool.Close()
		cancel()
	}

	return router, cleanup
}

func BenchmarkRouterWrite(b *testing.B) {
	router, cleanup := setupBenchmarkRouter(b, "bench-router")
	defer cleanup()

	key := []byte("bench-router-key")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := router.Write(key); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
}

func BenchmarkRouterWriteSync(b *testing.B) {
	router, cleanup := setupBenchmarkRouter(b, "bench-router-sync")
	defer cleanup()

	key := []byte("bench-router-sync-key")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := router.WriteSync(key); err != nil {
			b.Fatalf("write sync: %v", err)
		}
	}
}

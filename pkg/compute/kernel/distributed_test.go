package kernel

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/remote"
)

func TestDistributedBackendNoPeers(t *testing.T) {
	Convey("Given a distributed backend with no peers", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		backend, err := NewDistributedBackend(
			DistributedWithContext(ctx),
		)
		So(err, ShouldBeNil)
		So(backend, ShouldNotBeNil)
		defer backend.Close()

		Convey("Available should report zero", func() {
			n, availErr := backend.Available()
			So(availErr, ShouldBeNil)
			So(n, ShouldEqual, 0)
		})

		Convey("Write should return ErrNoPeers when no node is reachable", func() {
			_, writeErr := backend.Write([]byte("key"))
			So(writeErr, ShouldEqual, remote.ErrNoPeers)
		})

		Convey("Read should return ErrNoPeers when no node is reachable", func() {
			buf := make([]byte, 32)
			_, readErr := backend.Read(buf)
			So(readErr, ShouldEqual, remote.ErrNoPeers)
		})
	})
}

func TestDistributedBackendAddPeer(t *testing.T) {
	Convey("Given a distributed backend", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		backend, err := NewDistributedBackend(
			DistributedWithContext(ctx),
		)
		So(err, ShouldBeNil)
		defer backend.Close()

		Convey("When adding a peer address", func() {
			addErr := backend.AddPeer("127.0.0.1:9999")
			So(addErr, ShouldBeNil)

			Convey("It should appear in the router's node list", func() {
				nodes := backend.Router().Nodes()
				So(len(nodes), ShouldEqual, 1)
				So(nodes[0].Addr(), ShouldEqual, "127.0.0.1:9999")
			})
		})
	})
}

func TestDistributedBackendWithRouter(t *testing.T) {
	Convey("Given a distributed backend with an injected router", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		router := remote.NewRouter(
			remote.RouterWithContext(ctx),
		)

		backend, err := NewDistributedBackend(
			DistributedWithContext(ctx),
			DistributedWithRouter(router),
		)
		So(err, ShouldBeNil)
		defer backend.Close()

		Convey("It should use the injected router", func() {
			So(backend.Router(), ShouldEqual, router)
		})
	})
}

func TestDistributedBackendWriteToUnreachablePeer(t *testing.T) {
	Convey("Given a distributed backend with an unreachable peer", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		backend, err := NewDistributedBackend(
			DistributedWithContext(ctx),
			DistributedWithCluster(nil),
			DistributedWithPeers([]string{"127.0.0.1:59999"}),
		)
		So(err, ShouldBeNil)
		defer backend.Close()

		Convey("Write should return a connection error", func() {
			_, writeErr := backend.Write([]byte("key"))
			So(writeErr, ShouldNotBeNil)
		})
	})
}

func TestStartDistributed(t *testing.T) {
	Convey("Given a context and no peers", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		Convey("StartDistributed should return a valid backend", func() {
			backend, err := StartDistributed(ctx, nil, nil)
			So(err, ShouldBeNil)
			So(backend, ShouldNotBeNil)
			defer backend.Close()

			n, availErr := backend.Available()
			So(availErr, ShouldBeNil)
			So(n, ShouldEqual, 0)
		})
	})
}

func BenchmarkDistributedBackendAvailable(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend, _ := NewDistributedBackend(
		DistributedWithContext(ctx),
	)
	defer backend.Close()

	b.ReportAllocs()

	for b.Loop() {
		backend.Available()
	}
}

func BenchmarkDistributedBackendAddPeer(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.ReportAllocs()

	for b.Loop() {
		backend, _ := NewDistributedBackend(
			DistributedWithContext(ctx),
		)

		backend.AddPeer("127.0.0.1:9999")
		backend.Close()
	}
}

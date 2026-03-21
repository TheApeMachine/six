package vm

import (
	"context"
	"testing"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/logic/synthesis"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
TestBooterRegistersCapabilities verifies that every expected capability
is resolvable through the router after boot.
*/
func TestBooterRegistersCapabilities(t *testing.T) {
	gc.Convey("Given a Booter with runtime dependencies", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 1, &pool.Config{})
		defer workerPool.Close()

		broadcast := pool.NewBroadcastGroup("booter-test", time.Second, 16)
		defer broadcast.Close()

		booter := NewBooter(
			BooterWithContext(ctx),
			BooterWithPool(workerPool),
			BooterWithBroadcast(broadcast),
		)

		defer booter.Close()

		gc.Convey("It should resolve all registered capabilities via the router", func() {
			raw, err := booter.router.Get(ctx, cluster.GRAPH, "test")
			gc.So(err, gc.ShouldBeNil)
			gc.So(substrate.Graph(raw).IsValid(), gc.ShouldBeTrue)

			raw, err = booter.router.Get(ctx, cluster.HAS, "test")
			gc.So(err, gc.ShouldBeNil)
			gc.So(synthesis.HAS(raw).IsValid(), gc.ShouldBeTrue)

			raw, err = booter.router.Get(ctx, cluster.CANTILEVER, "test")
			gc.So(err, gc.ShouldBeNil)
			gc.So(bvp.Cantilever(raw).IsValid(), gc.ShouldBeTrue)

			raw, err = booter.router.Get(ctx, cluster.MACROINDEX, "test")
			gc.So(err, gc.ShouldBeNil)
			gc.So(macro.MacroIndex(raw).IsValid(), gc.ShouldBeTrue)
		})
	})
}

/*
BenchmarkNewBooter measures boot wiring cost for all runtime servers.
*/
func BenchmarkNewBooter(b *testing.B) {
	for b.Loop() {
		ctx, cancel := context.WithCancel(context.Background())

		workerPool := pool.New(ctx, 1, 1, &pool.Config{})
		broadcast := pool.NewBroadcastGroup("booter-bench", time.Second, 16)

		booter := NewBooter(
			BooterWithContext(ctx),
			BooterWithPool(workerPool),
			BooterWithBroadcast(broadcast),
		)

		booter.Close()
		broadcast.Close()
		workerPool.Close()
		cancel()
	}
}

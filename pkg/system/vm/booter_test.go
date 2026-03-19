package vm

import (
	"context"
	"testing"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
TestBooterWiresSharedMacroIndex verifies that Graph and Cantilever share one
macro registry instance when Booter boots the runtime.
*/
func TestBooterWiresSharedMacroIndex(t *testing.T) {
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

		gc.Convey("Then Graph should receive the same shared macro index", func() {
			gc.So(booter.sharedIndex, gc.ShouldNotBeNil)
			gc.So(booter.graphServer, gc.ShouldNotBeNil)
			gc.So(booter.graphServer.MacroIndex(), gc.ShouldEqual, booter.sharedIndex)
			gc.So(booter.has.IsValid(), gc.ShouldBeTrue)
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

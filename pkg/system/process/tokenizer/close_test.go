package tokenizer

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/system/pool"
)

func TestUniversalServerCloseIsIdempotent(t *testing.T) {
	gc.Convey("Given a tokenizer server with an active RPC client", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 4, nil)
		server := newServer(ctx, workerPool)
		_ = server.Client("test")

		gc.Convey("Close should release the Cap'n Proto pipes without panicking, even twice", func() {
			gc.So(server.Close(), gc.ShouldBeNil)
			gc.So(server.Close(), gc.ShouldBeNil)
		})
	})
}

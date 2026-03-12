package process

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/pool"
)

func TestTokenizerServer(t *testing.T) {
	Convey("Given a TokenizerServer", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 1, 10, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)

		server := NewTokenizerServer(
			TokenizerWithContext(ctx),
			TokenizerWithPool(p),
			TokenizerWithBroadcast(broadcast),
		)

		Convey("When testing Done", func() {
			err := server.Done(ctx, Tokenizer_done{})
			Convey("It should return nil", func() {
				So(err, ShouldBeNil)
			})
		})

		Convey("When testing Receive", func() {
			// Construct a valid spatial client wrapped in a net.Conn simulation
			// For testing without full net plumbing (or using net.Pipe)
			// we just test it parses valid pool.Result

			// We skip injecting actual RPC client directly without capnp.Client wrapper
			// because creating an RPC net.Conn is complex for a simple unit test.
			// But we test the handling of the Result type.
			result := &pool.Result{
				Value: pool.PoolValue[[]data.Chord]{
					Key: "spatial_index",
					// Need a valid capability here, but we can't easily fake one
					// without rpc implementation. Receive handles pool.PoolValue[any].
				},
			}
			server.Receive(result) // It shouldn't panic
		})

		Convey("When testing Announce", func() {
			server.Announce()
			Convey("It should not panic", func() {
				So(true, ShouldBeTrue)
			})
		})

		Convey("When testing generate directly", func() {
			err := server.generate(ctx)

			Convey("It should schedule properly and not error", func() {
				So(err, ShouldBeNil)
			})
		})
	})
}

func BenchmarkTokenizerGenerate(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := pool.New(ctx, 1, 100, nil)
	server := NewTokenizerServer(TokenizerWithContext(ctx), TokenizerWithPool(p))

	raw := make([]byte, 1024)
	for i := range raw {
		raw[i] = byte(i % config.Numeric.VocabSize)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = server.generate(ctx)
	}
}

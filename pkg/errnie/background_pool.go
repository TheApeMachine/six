package errnie

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/system/pool"
)

var (
	backgroundPoolOnce sync.Once
	backgroundPool     *pool.Pool
	backgroundJobSeq   atomic.Uint64
)

func errnieBackgroundPool() *pool.Pool {
	backgroundPoolOnce.Do(func() {
		backgroundPool = pool.New(
			context.Background(),
			1,
			runtime.NumCPU(),
			&pool.Config{},
		)
	})

	return backgroundPool
}

/*
scheduleBackground enqueues a background job on errnieBackgroundPool with a unique
name using backgroundJobSeq. It passes a non-cancellable context via
pool.WithContext(context.Background()) and sets a 1s result TTL via pool.WithTTL.
The TTL controls job/result lifecycle in the pool, not goroutine preemption.
Any execution timeout must be handled cooperatively inside fn using its own
cancellable context if needed.
*/
func scheduleBackground(
	id string,
	fn func(context.Context) (any, error),
) {
	errnieBackgroundPool().Schedule(
		fmt.Sprintf("errnie/%s/%d", id, backgroundJobSeq.Add(1)),
		fn,
		pool.WithContext(context.Background()),
		pool.WithTTL(time.Second),
	)
}

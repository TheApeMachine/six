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

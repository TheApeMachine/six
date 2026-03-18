package provider

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/system/pool"
)

/*
RawToken represents a single token sample from a Dataset.
SampleID groups tokens that belong to the same logical sequence.
Symbol is the actual byte value.
Pos is the sequential position of the symbol within the sample.
*/
type RawToken struct {
	SampleID uint32
	Symbol   byte
	Pos      uint32
}

/*
Dataset represents a streaming source of generic token data.
Generate returns a channel of RawToken that streams token samples,
and the channel is closed by the Dataset when all tokens have been produced.
*/
type Dataset interface {
	Generate() chan RawToken
}

var (
	asyncPoolOnce sync.Once
	asyncPool     *pool.Pool
	asyncSeq      atomic.Uint64
)

func backgroundPool() *pool.Pool {
	asyncPoolOnce.Do(func() {
		asyncPool = pool.New(
			context.Background(),
			1,
			runtime.NumCPU(),
			&pool.Config{},
		)
	})

	return asyncPool
}

/*
AsyncTokens schedules token production on the shared provider pool and returns
the output channel immediately.
*/
func AsyncTokens(id string, fn func(chan<- RawToken)) chan RawToken {
	out := make(chan RawToken, 4096)

	backgroundPool().Schedule(
		fmt.Sprintf("provider/%s/%d", id, asyncSeq.Add(1)),
		func(ctx context.Context) (any, error) {
			defer close(out)
			fn(out)
			return nil, nil
		},
		pool.WithContext(context.Background()),
		pool.WithTTL(time.Second),
	)

	return out
}

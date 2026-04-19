package mesh

import (
	"context"
	"testing"

	"github.com/theapemachine/six/pkg/pool"
)

/*
newTestQueue returns a real pool.Queue for mesh tests (shared shape with
production Field construction).
*/
func newTestQueue(tb testing.TB) *pool.Queue {
	tb.Helper()

	queue, err := pool.NewQueue(context.Background())

	if err != nil {
		tb.Fatal(err)
	}

	return queue
}

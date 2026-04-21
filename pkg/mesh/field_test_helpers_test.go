package mesh

import (
	"context"
	"testing"

	"github.com/theapemachine/six/pkg/compute"
)

/*
newTestQueue returns a real pool.Queue for mesh tests (shared shape with
production Field construction).
*/
func newTestQueue(tb testing.TB) *compute.Queue {
	tb.Helper()

	queue := compute.NewQueue(context.Background())

	return queue
}

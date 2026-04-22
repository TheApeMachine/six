package network

import "context"

// ReadyTransport exposes a transport readiness phase before read/write.
// Implementations should be idempotent.
type ReadyTransport interface {
	Ready(ctx context.Context) error
}


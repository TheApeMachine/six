package cpu

import (
	"context"
	"runtime"
)

/*
Backend is the CPU substrate. It implements the unified HypercubeGossip
kernel for population-vectored AST execution.
*/
type Backend struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func NewBackend(ctx context.Context) *Backend {
	ctx, cancel := context.WithCancel(ctx)
	return &Backend{
		ctx:    ctx,
		cancel: cancel,
	}
}

func Available() int { return runtime.NumCPU() }

func (backend *Backend) Name() string { return "cpu" }

func (backend *Backend) Close() error {
	backend.cancel()
	return nil
}

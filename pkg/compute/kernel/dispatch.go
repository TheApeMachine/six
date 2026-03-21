package kernel

import (
	"io"
)

/*
Backend is the sole interface for kernel-level compute. Each implementation
(CPU, Metal, CUDA, distributed) provides an io.ReadWriteCloser surface
plus availability reporting. Data flows through Read/Write — the actual
transform logic lives in the Stream operations wired by the caller.
*/
type Backend interface {
	io.ReadWriteCloser
	Available() (int, error)
}

/*
Builder aggregates available Backend implementations and routes io to
the one with the highest reported capacity. It is itself an
io.ReadWriteCloser: Write goes to the best available backend, Read
comes back from it.

Backend selection is evaluated on every io call via Available() —
no TTL cache, no temporal drift. Backends that report higher capacity
counts are preferred over those with lower counts, enabling smooth
weighted routing instead of binary available/unavailable decisions.
*/
type Builder struct {
	backends    []Backend
	active      Backend
	activeIndex int
}

type builderOpts func(*Builder)

/*
WithBackend appends a backend to the priority list.
*/
func WithBackend(backend Backend) builderOpts {
	return func(builder *Builder) {
		builder.backends = append(builder.backends, backend)
	}
}

/*
NewBuilder creates a Builder with the given backends. The backend with
the highest Available() count wins on each io call. If none are provided,
the builder starts empty and all io returns EOF.
*/
func NewBuilder(opts ...builderOpts) *Builder {
	builder := &Builder{
		backends:    make([]Backend, 0, 4),
		activeIndex: -1,
	}

	for _, opt := range opts {
		opt(builder)
	}

	return builder
}

/*
Reset clears the cached active backend so the next io call re-selects.
*/
func (builder *Builder) Reset() {
	builder.active = nil
	builder.activeIndex = -1
}

/*
best selects the backend with the highest available capacity on every
call. No TTL cache — the availability probe runs against the live
backend state each time, eliminating temporal drift between the
dispatcher and the physical hardware queues.
*/
func (builder *Builder) best() Backend {
	bestIndex := -1
	bestCount := 0

	for index, backend := range builder.backends {
		count, err := backend.Available()

		if err != nil || count <= 0 {
			continue
		}

		if count > bestCount {
			bestCount = count
			bestIndex = index
		}
	}

	if bestIndex < 0 {
		builder.active = nil
		builder.activeIndex = -1
		return nil
	}

	builder.active = builder.backends[bestIndex]
	builder.activeIndex = bestIndex

	return builder.active
}

/*
Read delegates to the best available backend.
*/
func (builder *Builder) Read(p []byte) (n int, err error) {
	backend := builder.best()

	if backend == nil {
		return 0, io.EOF
	}

	return backend.Read(p)
}

/*
Write delegates to the best available backend.
*/
func (builder *Builder) Write(p []byte) (n int, err error) {
	backend := builder.best()

	if backend == nil {
		return 0, io.ErrClosedPipe
	}

	return backend.Write(p)
}

/*
Close delegates to the best available backend.
*/
func (builder *Builder) Close() error {
	backend := builder.best()

	if backend == nil {
		return nil
	}

	return backend.Close()
}

/*
Available returns the total availability across all backends.
*/
func (builder *Builder) Available() (int, error) {
	total := 0

	for _, backend := range builder.backends {
		count, err := backend.Available()

		if err != nil {
			return 0, err
		}

		total += count
	}

	return total, nil
}

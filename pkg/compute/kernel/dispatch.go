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
the best one. It is itself an io.ReadWriteCloser: Write goes to the
highest-priority available backend, Read comes back from it.
*/
type Builder struct {
	backends []Backend
	active   Backend
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
NewBuilder creates a Builder with the given backends. The first backend
that reports Available > 0 wins on each io call. If none are provided,
the builder starts empty and all io returns EOF.
*/
func NewBuilder(opts ...builderOpts) *Builder {
	builder := &Builder{
		backends: make([]Backend, 0, 4),
	}

	for _, opt := range opts {
		opt(builder)
	}

	return builder
}

/*
Reset clears the cached active backend so the next io call re-selects from
builder.backends.
*/
func (builder *Builder) Reset() {
	builder.active = nil
}

/*
best returns the first backend that reports Available > 0, caching the
result in active. If the cached active fails a fresh Available check, it
is cleared and selection runs again. Returns nil when none qualify.
*/
func (builder *Builder) best() Backend {
	if builder.active != nil {
		n, err := builder.active.Available()

		if err != nil || n <= 0 {
			builder.active = nil
		} else {
			return builder.active
		}
	}

	for _, backend := range builder.backends {
		n, err := backend.Available()

		if err != nil {
			continue
		}

		if n > 0 {
			builder.active = backend
			return backend
		}
	}

	return nil
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
		n, err := backend.Available()

		if err != nil {
			return 0, err
		}

		total += n
	}

	return total, nil
}

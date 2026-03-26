package compute

import (
	"errors"
	"io"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Backend acts as an intelligent Multi-Substrate Load Balancer. It monitors
pressure across available local arithmetic hardware (GPU/CPU) and geometrically
overflows into the local Region (which acts as a local clustering space and
network mesh interface) if local capabilities are fully saturated.
*/
type Backend struct {
	hardware   []kernel.Substrate
	meshRegion *primitive.Region
}

// BackendOption configures the multi-substrate router
type BackendOption func(*Backend)

// WithRegion hooks the affine topological mesh over the execution hardware
func WithRegion(region *primitive.Region) BackendOption {
	return func(b *Backend) {
		b.meshRegion = region
	}
}

/*
NewBackend initializes the unified Load Balancer by probing for
all available compute substrates and layering them by speed priority.
*/
func NewBackend(opts ...BackendOption) *Backend {
	b := &Backend{
		hardware: make([]kernel.Substrate, 0),
	}

	if cuda.Available() > 0 {
		errnie.Info("compute.backend: CUDA substrate registered", "priority", 1)
		b.hardware = append(b.hardware, cuda.NewBackend())
	}

	if metal.Available() > 0 {
		errnie.Info("compute.backend: Metal substrate registered", "priority", 2)
		b.hardware = append(b.hardware, metal.NewBackend())
	}

	errnie.Info("compute.backend: CPU substrate registered", "priority", 3)
	b.hardware = append(b.hardware, cpu.NewBackend())

	for _, opt := range opts {
		opt(b)
	}

	return b
}

/*
Read polls all attached hardware architectures and the affine regional mesh
for completed execution residues, ensuring a rapid O(1) non-blocking return
that feeds back into the VM pipeline.
*/
func (backend *Backend) Read(p []byte) (n int, err error) {
	if len(p) < primitive.ByteSize {
		return 0, io.ErrShortBuffer
	}

	// 1. Try to pluck frames off local hardware
	for _, hw := range backend.hardware {
		n, err = hw.Read(p)
		if n > 0 && (err == nil || err == io.EOF) {
			return n, nil // Found data! Return it instantly.
		}
	}

	// 2. Try to pull trapped firmware programs or search tombstones off the mesh
	if backend.meshRegion != nil {
		n, err = backend.meshRegion.Read(p)
		if n > 0 && (err == nil || err == io.EOF) {
			return n, nil
		}
	}

	return 0, io.EOF
}

/*
Write pushes raw 1024-byte Values onto the execution bus. It applies
backpressure routing: utilizing the fastest available ALU first, and cascading
downwards into the geometric mesh if the node's local silicone is saturated.
*/
func (backend *Backend) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	total := 0

	// Push in chunks to track throughput
	for len(p) > 0 {
		var written int
		var pushErr error
		dispatched := false

		// Try standard hardware
		for _, hw := range backend.hardware {
			written, pushErr = hw.Write(p)
			if pushErr == nil && written > 0 {
				dispatched = true
				break // Success!
			}
		}

		if !dispatched && backend.meshRegion != nil {
			errnie.Warn("compute.backend: ALU saturated! Spilling to region mesh", "size", len(p))
			written, pushErr = backend.meshRegion.Write(p)
			if pushErr == nil && written > 0 {
				dispatched = true
			}
		}

		if !dispatched {
			// Total architecture congestion. Return whatever was accomplished.
			return total, errors.New("compute.backend: complete node saturation, zero viable write targets")
		}

		p = p[written:]
		total += written
	}

	return total, nil
}

func (backend *Backend) Close() error {
	var errs error
	for _, hw := range backend.hardware {
		if err := hw.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	if backend.meshRegion != nil {
		if err := backend.meshRegion.Close(); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

/*
UniversalBitwise implements the raw memory dispatcher if bypassing the stream.
*/
func (backend *Backend) UniversalBitwise(a, b, dst unsafe.Pointer, n uint32) error {
	if len(backend.hardware) == 0 {
		return errors.New("compute.backend: no hardware kernel initialized")
	}
	return backend.hardware[0].UniversalBitwise(a, b, dst, n)
}

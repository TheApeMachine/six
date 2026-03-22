package compute

import (
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/transport"
)

type Backend struct {
	*transport.Stream
	cpu   *cpu.Backend
	cuda  *cuda.Backend
	metal *metal.Backend
}

func NewBackend() *Backend {
	return &Backend{
		Stream: transport.NewStream(),
		cpu:    cpu.NewBackend(),
		cuda:   cuda.NewBackend(),
		metal:  metal.NewBackend(),
	}
}

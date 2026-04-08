package causal

import "github.com/theapemachine/six/pkg/primitive"

/*
InterventionTarget is the minimal surface Pearl-style operations need from
an execution frame. primitive.Value implements it on *Value; lightweight
tests or simulators can supply their own types without allocating a full
1KiB hardware-aligned frame.
*/
type InterventionTarget interface {
	BindContext(binding [primitive.AffinityWords]uint64)
	GradientVector() [primitive.RegionWords]uint64
	AccumulateGradient(residual [primitive.RegionWords]uint64)
	AffinityVector() [primitive.AffinityWords]uint64
}

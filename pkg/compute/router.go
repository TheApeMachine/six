package compute

import "github.com/theapemachine/six/pkg/primitive"

/*
AffinityRouter is the mesh/compute boundary for packed community routing.
mesh.Field owns policy, while compute owns how distance vectors are produced
from the packed seed table on CPU, Metal, or CUDA.
*/
type AffinityRouter interface {
	AffinityDistances(
		query *[primitive.AffinityWords]uint64,
		candidates [][primitive.AffinityWords]uint64,
	) []uint32
}

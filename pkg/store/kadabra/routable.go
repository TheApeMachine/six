package kadabra

import "github.com/theapemachine/six/pkg/primitive"

/*
Routable is the interface that the Kadabra DHT routing layer requires
from storable values. It decouples the network layer from the concrete
primitive.Value memory layout.
*/
type Routable interface {
	/*
		AffinityVector returns the affinity region as a fixed-size array
		used for content-aware routing.
	*/
	AffinityVector() [primitive.AffinityWords]uint64

	/*
		String returns the textual content used for sequence storage
		and record hashing.
	*/
	String() string
}

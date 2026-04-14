package vm

import (
	"io"

	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/viz"
)

/*
Router is a component that routes Values to the most appropriate community field.
*/
type Router struct {
	field *geometry.Field
	route gossip.PriorityRoute
	distanceBudget int
}

/*
NewRouter creates a new router.
*/
func NewRouter() *Router {
	return &Router{
		distanceBudget: 10,
	}
}

/*
Route a settled Value to the most appropriate community field
using the gossip-based fast path. It wraps the PriorityRoute in an
AffinityFilter and copies the Value. If no community accepts the Value,
a new community is spawned and added to the route.
*/
func (router *Router) Route(values ...*primitive.Value) {
	const distanceBudget = 10

	for _, value := range values {
		community := router.FindCommunity(value)

		if community == nil {
			newCommunity := geometry.NewField(geometry.Mod8191)
			newCommunity.MergeAffinity(value.Get(primitive.AffinityRegion))
			router.route.AddPeer(newCommunity, value.Get(primitive.AffinityRegion)[:])
			community = newCommunity
		}

		community.Values = append(community.Values, value)
	}
}

/*
FindCommunity finds the most appropriate community for a Value.
*/
func (router *Router) FindCommunity(value *primitive.Value) *geometry.Field {
	var frameAffinity [5]uint64
	copy(frameAffinity[:], value.Get(primitive.AffinityRegion))

	// Try to route to an existing community in priority order
	for _, peer := range router.route {
		peerAffinity := peer.Affinity()
		dist := geometry.AffinityHammingDistance(frameAffinity[:], peerAffinity[:])

		if dist <= router.distanceBudget {
			// We found a community within budget. Use AffinityFilter to write it.
			// Since we already checked the distance, we could just write directly to peer.Dst(),
			// but we'll use AffinityFilter to follow the proposal's composable I/O pattern.
			filter := gossip.NewAffinityFilter(peer.Dst(), peerAffinity, router.distanceBudget)

			// Value implements io.Reader, so we can use io.Copy.
			// However, io.Copy will consume the Value's reader.
			// Since we only want to write it once, and value.Read 
			// returns EOF after one frame, this is perfect.
			if _, err := io.Copy(filter, value); err != nil {
				errnie.Error(err)

				return nil
			}

			if communityField, ok := peer.Dst().(*geometry.Field); ok {
				communityField.MergeAffinity(frameAffinity[:])
			}

			if viz.DefaultBus.IsActive() {
				// Find the community ID for the visualizer
				communityID := -1

				for i, fieldValue := range router.field.Fields {
					if fieldValue == peer.Dst() {
						communityID = i
						break
					}
				}

				if communityID != -1 {
					viz.DefaultBus.Publish(
						viz.ValueJoinedCommunityEvent(
							value.ID(), communityID, dist,
						),
					)
				}
			}

			return nil
		}
	}

	return nil
}

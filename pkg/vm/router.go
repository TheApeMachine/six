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
Router routes Values to the most appropriate community field using affinity
distance as the metric. Communities are tracked by sequential ID so the
visualization layer can display them as discrete clusters.
*/
type Router struct {
	route          gossip.PriorityRoute
	communityIDs   map[io.ReadWriteCloser]int
	nextID         int
	distanceBudget int
	global         *geometry.Field
}

/*
NewRouter creates a new router with an empty community registry.
*/
func NewRouter(global *geometry.Field) *Router {
	return &Router{
		communityIDs:   make(map[io.ReadWriteCloser]int),
		distanceBudget: 60,
		global:         global,
	}
}

/*
Route a settled Value to the most appropriate community field
using the gossip-based fast path. It wraps the PriorityRoute in an
AffinityFilter and copies the Value. If no community accepts the Value,
a new community is spawned and added to the route.
*/
func (router *Router) Route(values ...*primitive.Value) []*geometry.Field {
	assigned := make([]*geometry.Field, 0, len(values))

	for _, value := range values {
		community := router.findCommunity(value)

		if community == nil {
			community = router.spawnCommunity(value)
		}

		community.Values = append(community.Values, value)
		assigned = append(assigned, community)
	}

	return assigned
}

/*
spawnCommunity creates a new community field from the Value's affinity,
registers it in the route, assigns a stable ID, and emits the viz event.
*/
func (router *Router) spawnCommunity(value *primitive.Value) *geometry.Field {
	community := geometry.NewCommunityField(geometry.Mod8191)
	affinity := value.Get(primitive.AffinityRegion)
	community.MergeAffinity(affinity)

	router.route.AddPeer(community, affinity[:])

	cid := router.nextID
	router.communityIDs[community] = cid
	router.nextID++
	router.registerCommunity(cid, community, affinity)

	if viz.DefaultBus.IsActive() {
		viz.DefaultBus.Publish(viz.CommunityCreatedEvent(cid, affinity[:]))
		viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(value.ID(), cid, 0))
	}

	return community
}

/*
findCommunity finds the most appropriate community for a Value within the
distance budget, writes the Value into it via AffinityFilter, and emits
routing viz events.
*/
func (router *Router) findCommunity(value *primitive.Value) *geometry.Field {
	var frameAffinity [5]uint64
	copy(frameAffinity[:], value.Get(primitive.AffinityRegion))

	for _, peer := range router.route {
		peerAffinity := peer.Affinity()
		dist := geometry.AffinityHammingDistance(frameAffinity[:], peerAffinity[:])

		if dist > router.distanceBudget {
			continue
		}

		filter := gossip.NewAffinityFilter(peer.Dst(), peerAffinity, router.distanceBudget)

		if _, err := io.Copy(filter, value); err != nil {
			errnie.Error(err)
			return nil
		}

		communityField, ok := peer.Dst().(*geometry.Field)
		if !ok {
			return nil
		}

		communityField.MergeAffinity(frameAffinity[:])
		router.mergeGlobal(frameAffinity[:])

		if viz.DefaultBus.IsActive() {
			cid, known := router.communityIDs[peer.Dst()]
			if known {
				viz.DefaultBus.Publish(viz.ValueJoinedCommunityEvent(
					value.ID(), cid, dist,
				))
			}
		}

		return communityField
	}

	return nil
}

/*
CommunityCount reports the number of routed community fields known to this router.
*/
func (router *Router) CommunityCount() int {
	if router == nil {
		return 0
	}

	return router.nextID
}

func (router *Router) registerCommunity(
	communityID int,
	community *geometry.Field,
	affinity []uint64,
) {
	if router == nil || router.global == nil || community == nil {
		return
	}

	if communityID >= 0 && communityID < len(router.global.Fields) {
		router.global.Fields[communityID] = community
	}

	router.mergeGlobal(affinity)
}

func (router *Router) mergeGlobal(affinity []uint64) {
	if router == nil || router.global == nil {
		return
	}

	router.global.MergeAffinity(affinity)
}

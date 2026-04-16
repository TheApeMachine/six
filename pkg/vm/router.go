package vm

import (
	"fmt"
	"io"
	"strings"

	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
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
	// affinityBuckets maps a coarse 8-bit projection of the first affinity
	// word so routing probes likely communities before scanning the tail.
	affinityBuckets [256][]*geometry.Field
	// allCommunities is the append-only registry used to build the VP tree.
	allCommunities []*geometry.Field
	vpRoot         *affinityVPNode
	vpNeedsRebuild bool
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
		if existing := router.assignedCommunity(value); existing != nil {
			assigned = append(assigned, existing)
			continue
		}

		community := router.findCommunity(value)

		if community == nil {
			community = router.spawnCommunity(value)
		}

		community.Values = append(community.Values, value)
		assigned = append(assigned, community)
	}

	return assigned
}

func (router *Router) assignedCommunity(value *primitive.Value) *geometry.Field {
	if router == nil || router.global == nil || value == nil {
		return nil
	}

	for _, community := range router.global.Fields {
		if community == nil {
			continue
		}

		for _, member := range community.Values {
			if member == value || (member != nil && member.ID() == value.ID()) {
				return community
			}
		}
	}

	return nil
}

/*
PublishGraphSnapshot emits the current routed community membership graph.
*/
func (router *Router) PublishGraphSnapshot() {
	if router == nil || router.global == nil || !telemetry.DefaultBus.IsActive() {
		return
	}

	communities := make([]telemetry.CommunityGraphSnapshot, 0, len(router.global.Fields))

	for communityID, community := range router.global.Fields {
		if community == nil {
			continue
		}

		snapshot := telemetry.CommunityGraphSnapshot{
			ID:          communityID,
			AffinityHex: affinityHex(community.Affinity),
			MemberIDs:   make([]string, 0, len(community.Values)),
		}

		for _, value := range community.Values {
			if value == nil {
				continue
			}

			snapshot.MemberIDs = append(snapshot.MemberIDs, telemetry.FormatValueIDHex(value.ID()))
		}

		communities = append(communities, snapshot)
	}

	telemetry.DefaultBus.Publish(telemetry.TrieGraphSnapshotEvent(communities))
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

	if telemetry.DefaultBus.IsActive() {
		telemetry.DefaultBus.Publish(telemetry.CommunityCreatedEvent(cid, affinity[:]))
		telemetry.DefaultBus.Publish(telemetry.ValueJoinedCommunityEvent(value.ID(), cid, 0))
	}

	bucket := affinityBucketKey(affinity)
	router.affinityBuckets[bucket] = append(router.affinityBuckets[bucket], community)

	router.allCommunities = append(router.allCommunities, community)
	router.vpNeedsRebuild = true

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

	if len(router.allCommunities) >= affinityVPThreshold {
		if router.vpRoot == nil || router.vpNeedsRebuild {
			router.vpRoot = buildAffinityVP(append([]*geometry.Field(nil), router.allCommunities...))
			router.vpNeedsRebuild = false
		}

		best, dist := nearestCommunityWithin(router.vpRoot, frameAffinity, router.distanceBudget)
		if best != nil {
			return router.finishJoin(best, frameAffinity, value, dist)
		}
	}

	primary := affinityBucketKey(frameAffinity[:])

	if hit := router.tryAffinityBucket(frameAffinity, primary, value); hit != nil {
		return hit
	}

	for bucket := 0; bucket < 256; bucket++ {
		if uint8(bucket) == primary {
			continue
		}

		if hit := router.tryAffinityBucket(frameAffinity, uint8(bucket), value); hit != nil {
			return hit
		}
	}

	return nil
}

/*
finishJoin merges the routed affinity into a community and mirrors telemetry.
*/
func (router *Router) finishJoin(
	community *geometry.Field,
	frameAffinity [5]uint64,
	value *primitive.Value,
	dist int,
) *geometry.Field {
	if community == nil || router == nil {
		return nil
	}

	community.MergeAffinity(frameAffinity[:])
	router.mergeGlobal(frameAffinity[:])
	router.vpNeedsRebuild = true

	if telemetry.DefaultBus.IsActive() {
		cid, known := router.communityIDs[community]
		if known {
			telemetry.DefaultBus.Publish(telemetry.ValueJoinedCommunityEvent(
				value.ID(), cid, dist,
			))
		}
	}

	return community
}

func affinityBucketKey(affinity []uint64) uint8 {
	if len(affinity) == 0 {
		return 0
	}

	return uint8(affinity[0] >> 56)
}

func (router *Router) tryAffinityBucket(
	frameAffinity [5]uint64,
	bucket uint8,
	value *primitive.Value,
) *geometry.Field {
	if router == nil {
		return nil
	}

	for _, communityField := range router.affinityBuckets[bucket] {
		if communityField == nil {
			continue
		}

		peerAffinity := communityField.Affinity

		if len(peerAffinity) < 5 {
			continue
		}

		dist := geometry.AffinityHammingDistance(frameAffinity[:], peerAffinity[:5])

		if dist > router.distanceBudget {
			continue
		}

		return router.finishJoin(communityField, frameAffinity, value, dist)
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

	if communityID >= 0 {
		if !router.ensureGlobalSlot(communityID) {
			return
		}

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

func (router *Router) ensureGlobalSlot(communityID int) bool {
	if router == nil || router.global == nil || communityID < 0 {
		return false
	}

	if communityID < len(router.global.Fields) {
		return true
	}

	grown := make([]*geometry.Field, communityID+1)
	copy(grown, router.global.Fields)
	router.global.Fields = grown
	return true
}

func affinityHex(words []uint64) string {
	if len(words) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(words) * 16)

	for _, word := range words {
		fmt.Fprintf(&builder, "%016x", word)
	}

	return builder.String()
}

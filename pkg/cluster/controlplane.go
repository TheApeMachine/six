package cluster

import (
	"context"

	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
ControlPlane owns the Kademlia routing table and is the single entry point
for inserting Values into the cluster and querying for nearest neighbors.
Each k-bucket in the routing table has its own LSM (TokenID → Value bitmap).
*/
type ControlPlane struct {
	ctx    context.Context
	cancel context.CancelFunc
	rt     *RoutingTable
}

/*
NewControlPlane creates a ControlPlane. The local NodeID is derived from
the affinity of the first inserted Value; until then it uses 0 as a
placeholder (the routing table self-corrects on first Insert).
*/
func NewControlPlane(ctx context.Context) *ControlPlane {
	ctx, cancel := context.WithCancel(ctx)

	return &ControlPlane{
		ctx:    ctx,
		cancel: cancel,
		rt:     NewRoutingTable(0),
	}
}

/*
Insert adds a Value to the routing table and its bucket's LSM.
*/
func (cp *ControlPlane) Insert(key uint64, value primitive.Value) {
	// Bootstrap local ID from the first Value inserted.
	if !cp.rt.isBootstrapped() {
		cp.rt.SetLocal(NodeID(key))
	}

	cp.rt.Insert(key, &value)

	cp.rt.mu.RLock()
	localID := cp.rt.local
	cp.rt.mu.RUnlock()

	telemetry.Emit(telemetry.Event{
		Component: "LSM",
		Action:    "Insert",
		Data: telemetry.EventData{
			Stage:   "kademlia-route",
			NodeID:  key,
			Bin:     bucketIndex(localID, NodeID(key)),
			Message: "routed to k-bucket",
		},
	})
}

func (cp *ControlPlane) bucketsSnapshot() [IDBits]*kBucket {
	var buckets [IDBits]*kBucket

	if cp == nil || cp.rt == nil {
		return buckets
	}

	cp.rt.mu.RLock()
	buckets = cp.rt.buckets
	cp.rt.mu.RUnlock()

	return buckets
}

/*
FrameByValueID returns the frame stored by valueID across all k-buckets.
*/
func (cp *ControlPlane) FrameByValueID(valueID uint64) (frame [128]uint64, ok bool) {
	buckets := cp.bucketsSnapshot()

	for _, bucket := range buckets {
		if bucket == nil {
			continue
		}

		frame, ok = bucket.lsm.FrameByValueID(valueID)
		if ok {
			return frame, ok
		}
	}

	return [128]uint64{}, false
}

/*
LookupKeysByValue reverse-resolves token keys for the exact frame value match by
searching each bucket LSM.
*/
func (cp *ControlPlane) LookupKeysByValue(value *primitive.Value) []uint64 {
	buckets := cp.bucketsSnapshot()

	seen := make(map[uint64]struct{}, 16)
	out := make([]uint64, 0)

	for _, bucket := range buckets {
		if bucket == nil {
			continue
		}

		keys := bucket.lsm.LookupKeysByValue(value)
		for _, key := range keys {
			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}
			out = append(out, key)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

/*
LookupKeysByValueID collects token keys that index valueID across bucket LSMs.
*/
func (cp *ControlPlane) LookupKeysByValueID(valueID uint64) []uint64 {
	buckets := cp.bucketsSnapshot()

	seen := make(map[uint64]struct{}, 16)
	out := make([]uint64, 0)

	for _, bucket := range buckets {
		if bucket == nil {
			continue
		}

		keys := bucket.lsm.LookupKeysByValueID(valueID)
		for _, key := range keys {
			if _, exists := seen[key]; exists {
				continue
			}

			seen[key] = struct{}{}
			out = append(out, key)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

/*
FindClosest returns the K Values whose affinity is closest (by XOR distance)
to the target affinity.
*/
func (cp *ControlPlane) FindClosest(targetAffinity uint64) []primitive.Value {
	lookupCtx := cp.ctx

	if lookupCtx == nil {
		lookupCtx = context.Background()
	}

	entries := cp.rt.FindNode(lookupCtx, NodeID(targetAffinity))
	values := make([]primitive.Value, len(entries))

	for i, e := range entries {
		values[i] = *e.value
	}

	return values
}

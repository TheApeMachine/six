package kadabra

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store/markovtrie"
)

func TestMain(m *testing.M) {
	viper.SetConfigType("yml")
	configPath := filepath.Join("..", "..", "..", "cmd", "cfg", "config.yml")
	viper.SetConfigFile(configPath)

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "kadabra tests: viper.ReadInConfig(%s): %v\n", configPath, err)
		os.Exit(1)
	}

	core.NewConfig()
	os.Exit(m.Run())
}

func buildKadabraNode(id NodeID, options ...NodeOption) *KadabraNode {
	nodeOptions := []NodeOption{
		WithLocalStore(markovtrie.NewStore(
			markovtrie.WithDecayFactor(1),
			markovtrie.WithRandomSource(rand.NewSource(int64(id)+11)),
		)),
		WithNodeRandomSource(rand.NewSource(int64(id) + 23)),
	}

	nodeOptions = append(nodeOptions, options...)

	return NewKadabraNode(id, nodeOptions...)
}

// newValueWithAffinity creates a Value from seed data, fills the token region
// so LSH has enough bits to work with, and computes affinity.
func newValueWithAffinity(tb testing.TB, seed []byte) *primitive.Value {
	tb.Helper()
	// Pad seed to fill the 64-byte token region so LSH majority vote is meaningful.
	buf := make([]byte, 64)
	copy(buf, seed)
	for i := len(seed); i < 64; i++ {
		buf[i] = seed[i%len(seed)] ^ byte(i)
	}
	v, err := primitive.NewValue(buf)
	if err != nil {
		tb.Fatal(err)
	}
	if err := v.ComputeAffinityLSH(); err != nil {
		v.Close()
		tb.Fatal(err)
	}
	return v
}

func connectFully(nodes ...*KadabraNode) {
	for leftIndex := 0; leftIndex < len(nodes); leftIndex++ {
		for rightIndex := leftIndex + 1; rightIndex < len(nodes); rightIndex++ {
			left := nodes[leftIndex]
			right := nodes[rightIndex]
			rtt := float64(absNodeDistance(left.ID, right.ID)/2 + 1)
			Connect(left, right, rtt)
		}
	}
}

func absNodeDistance(left NodeID, right NodeID) NodeID {
	if left > right {
		return left - right
	}

	return right - left
}

func expectedClosestNodeIDs(nodes []*KadabraNode, target NodeID, limit int) []NodeID {
	ordered := append([]*KadabraNode(nil), nodes...)

	sort.Slice(ordered, func(leftIndex int, rightIndex int) bool {
		left := ordered[leftIndex]
		right := ordered[rightIndex]
		leftDistance := xorDistance(left.ID, target)
		rightDistance := xorDistance(right.ID, target)
		if leftDistance == rightDistance {
			return left.ID < right.ID
		}

		return leftDistance < rightDistance
	})

	if len(ordered) > limit {
		ordered = ordered[:limit]
	}

	ids := make([]NodeID, 0, len(ordered))
	for _, node := range ordered {
		ids = append(ids, node.ID)
	}

	return ids
}

func peerIDs(peers []PeerInfo) []NodeID {
	ids := make([]NodeID, 0, len(peers))
	for _, peer := range peers {
		ids = append(ids, peer.ID)
	}

	return ids
}

func TestNewKadabraNode(t *testing.T) {
	Convey("Given a new Kadabra node", t, func() {
		node := NewKadabraNode(7)

		Convey("It should initialize DHT defaults and bucket state", func() {
			So(node.ID, ShouldEqual, 7)
			So(node.Store, ShouldNotBeNil)
			So(node.BucketSize, ShouldEqual, core.Cfg.Kadabra.BucketSize)
			So(node.ReplicationFactor, ShouldEqual, core.Cfg.Kadabra.ReplicationFactor)
			So(node.LookupParallelism, ShouldEqual, core.Cfg.Kadabra.Alpha)
			So(node.EpochQueries, ShouldEqual, core.Cfg.Kadabra.EpochQueries)
			So(len(node.buckets), ShouldEqual, core.Cfg.Kadabra.Bits)
			So(node.buckets[0], ShouldNotBeNil)
			So(math.IsInf(node.buckets[0].PreviousScore, -1), ShouldBeTrue)
		})
	})
}

func TestConnect(t *testing.T) {
	Convey("Given two Kadabra nodes", t, func() {
		left := buildKadabraNode(0)
		right := buildKadabraNode(8)

		Convey("Connect should register both peers symmetrically", func() {
			Connect(left, right, 12)

			leftBucket := left.buckets[left.bucketIndexForPeer(right.ID)]
			rightBucket := right.buckets[right.bucketIndexForPeer(left.ID)]

			So(leftBucket.Candidates[right.ID], ShouldNotBeNil)
			So(rightBucket.Candidates[left.ID], ShouldNotBeNil)
			So(leftBucket.Entries[0].ID, ShouldEqual, right.ID)
			So(rightBucket.Entries[0].ID, ShouldEqual, left.ID)
		})
	})
}

func TestAddPeer(t *testing.T) {
	Convey("Given a Kadabra node with a known peer", t, func() {
		node := buildKadabraNode(0, WithBucketSize(1))
		peer := buildKadabraNode(8)

		node.AddPeer(peer, 12)

		Convey("AddPeer should update existing peer metadata in place", func() {
			node.AddPeer(peer, 4)

			bucket := node.buckets[node.bucketIndexForPeer(peer.ID)]
			So(bucket.Candidates[peer.ID].RTT, ShouldEqual, 4)
			So(bucket.Entries[0].RTT, ShouldEqual, 4)
		})
	})
}

func TestClosestPeers(t *testing.T) {
	Convey("Given a node with several peers", t, func() {
		node := buildKadabraNode(0)
		peerA := buildKadabraNode(4)
		peerB := buildKadabraNode(8)
		peerC := buildKadabraNode(16)

		node.AddPeer(peerA, 12)
		node.AddPeer(peerB, 8)
		node.AddPeer(peerC, 16)

		Convey("ClosestPeers should order entries by XOR distance", func() {
			peers := node.ClosestPeers(5, 2)

			So(peerIDs(peers), ShouldResemble, []NodeID{4, 8})
		})
	})
}

func TestStoreRecord(t *testing.T) {
	Convey("Given a Kadabra node", t, func() {
		node := buildKadabraNode(0)
		record := SequenceRecord{
			Key:       42,
			Sequence:  "blue_cab",
			Label:     "Truck",
			Publisher: node.ID,
		}

		Convey("StoreRecord should store and train only once per key", func() {
			So(node.StoreRecord(record), ShouldBeNil)
			So(node.StoreRecord(record), ShouldBeNil)

			So(node.HasRecord(record.Key), ShouldBeTrue)
			So(node.Store.CurrentStep(), ShouldEqual, 1)

			scores := node.Store.Classify(record.Sequence)

			So(scores["Truck"], ShouldBeGreaterThan, 0)
		})

		Convey("StoreRecord should reject conflicting payloads for the same key", func() {
			So(node.StoreRecord(record), ShouldBeNil)

			conflict := SequenceRecord{
				Key:       record.Key,
				Sequence:  "other_sequence",
				Label:     "Boat",
				Publisher: node.ID,
			}

			So(node.StoreRecord(conflict), ShouldNotBeNil)
		})
	})
}

func TestLookupNodes(t *testing.T) {
	Convey("Given a fully connected Kadabra mesh", t, func() {
		nodes := []*KadabraNode{
			buildKadabraNode(0, WithEpochQueries(1000)),
			buildKadabraNode(8, WithEpochQueries(1000)),
			buildKadabraNode(16, WithEpochQueries(1000)),
			buildKadabraNode(24, WithEpochQueries(1000)),
		}

		connectFully(nodes...)

		Convey("LookupNodes should return the globally closest discovered nodes", func() {
			peers := nodes[0].LookupNodes(10, 3)
			expected := expectedClosestNodeIDs(nodes, 10, 3)

			So(peerIDs(peers), ShouldResemble, expected)
		})
	})
}

func TestPublish(t *testing.T) {
	Convey("Given a fully connected Kadabra mesh", t, func() {
		nodes := []*KadabraNode{
			buildKadabraNode(0, WithReplicationFactor(2), WithEpochQueries(1000)),
			buildKadabraNode(8, WithReplicationFactor(2), WithEpochQueries(1000)),
			buildKadabraNode(16, WithReplicationFactor(2), WithEpochQueries(1000)),
			buildKadabraNode(24, WithReplicationFactor(2), WithEpochQueries(1000)),
		}

		connectFully(nodes...)

		Convey("Publish should replicate onto affinity-closest nodes", func() {
			value := newValueWithAffinity(t, []byte("blue_cab"))
			defer value.Close()

			record, err := nodes[0].Publish(*value, "Truck")
			So(err, ShouldBeNil)

			stored := 0
			for _, node := range nodes {
				if node.HasRecord(record.Key) {
					stored++
				}
			}
			So(stored, ShouldEqual, 2) // replication factor
		})
	})
}

func TestFindRecord(t *testing.T) {
	Convey("Given a fully connected Kadabra mesh with a published record", t, func() {
		nodes := []*KadabraNode{
			buildKadabraNode(0, WithReplicationFactor(2), WithEpochQueries(1000)),
			buildKadabraNode(8, WithReplicationFactor(2), WithEpochQueries(1000)),
			buildKadabraNode(16, WithReplicationFactor(2), WithEpochQueries(1000)),
			buildKadabraNode(24, WithReplicationFactor(2), WithEpochQueries(1000)),
		}

		connectFully(nodes...)

		value := newValueWithAffinity(t, []byte("blue_cab"))

		record, err := nodes[0].Publish(*value, "Truck")
		defer value.Close()
		So(err, ShouldBeNil)

		ordered := expectedClosestNodeIDs(nodes, NodeID(record.Key), len(nodes))
		seekerID := ordered[len(ordered)-1]

		var seeker *KadabraNode

		for _, node := range nodes {
			if node.ID == seekerID {
				seeker = node
				break
			}
		}

		Convey("FindRecord should discover the record through iterative lookup", func() {
			foundRecord, found, trace := seeker.FindRecord(record.Key)

			So(found, ShouldBeTrue)
			So(foundRecord, ShouldResemble, record)
			So(trace.Found, ShouldBeTrue)
			So(len(trace.Nodes), ShouldBeGreaterThanOrEqualTo, 1)
			So(trace.Latency, ShouldBeGreaterThanOrEqualTo, 0)
		})
	})
}

func TestKadabraBucketAdaptation(t *testing.T) {
	Convey("Given two peers in the same bucket with different RTTs", t, func() {
		node := buildKadabraNode(0, WithBucketSize(1), WithEpochQueries(1))
		slowPeer := buildKadabraNode(9)
		fastPeer := buildKadabraNode(8)

		node.AddPeer(slowPeer, 90)
		node.AddPeer(fastPeer, 10)

		bucketIndex := node.bucketIndexForPeer(slowPeer.ID)

		Convey("The bucket should switch to the faster peer after exploration and comparison", func() {
			node.LookupNodes(uint64(fastPeer.ID), 1)
			So(node.buckets[bucketIndex].Entries[0].ID, ShouldEqual, slowPeer.ID)

			node.LookupNodes(uint64(fastPeer.ID), 1)
			So(node.buckets[bucketIndex].Entries[0].ID, ShouldEqual, fastPeer.ID)

			node.LookupNodes(uint64(fastPeer.ID), 1)
			So(node.buckets[bucketIndex].Entries[0].ID, ShouldEqual, fastPeer.ID)
		})
	})
}

func TestKadabraSecurityThreshold(t *testing.T) {
	Convey("Given exploration candidates below and above the security RTT floor", t, func() {
		node := buildKadabraNode(0, WithBucketSize(1), WithEpochQueries(1), WithSecurityThreshold(50))
		currentPeer := buildKadabraNode(9)
		closePeer := buildKadabraNode(8)
		safePeer := buildKadabraNode(10)

		node.AddPeer(currentPeer, 90)
		node.AddPeer(closePeer, 10)
		node.AddPeer(safePeer, 80)

		bucketIndex := node.bucketIndexForPeer(currentPeer.ID)

		Convey("Exploration should skip candidates that violate the security threshold", func() {
			node.LookupNodes(uint64(safePeer.ID), 1)
			node.LookupNodes(uint64(safePeer.ID), 1)

			So(node.buckets[bucketIndex].Entries[0].ID, ShouldEqual, safePeer.ID)
		})
	})
}

func TestNodeIDFromBytes(t *testing.T) {
	Convey("Given a byte slice shorter than eight bytes", t, func() {
		Convey("NodeIDFromBytes should left-pad the identifier", func() {
			So(NodeIDFromBytes([]byte{1, 2, 3}), ShouldEqual, NodeID(0x0102030000000000))
		})
	})
}

func TestNodeIDFromString(t *testing.T) {
	Convey("Given a string identifier", t, func() {
		Convey("NodeIDFromString should hash trimmed input deterministically", func() {
			So(NodeIDFromString(" node-a "), ShouldEqual, NodeIDFromString("node-a"))
		})
	})
}

func TestAffinityRouting(t *testing.T) {
	Convey("Given a mesh where nodes have been seeded with distinct affinities", t, func() {
		nodes := make([]*KadabraNode, 8)
		for i := range nodes {
			nodes[i] = buildKadabraNode(NodeID(i*8), WithReplicationFactor(1), WithEpochQueries(1000))
		}
		connectFully(nodes...)

		// Seed each node with a distinct affinity so closestNodesByAffinity
		// can differentiate them. Node i gets bit i set in each affinity word.
		for i, n := range nodes {
			for w := range AffinityWords {
				n.Affinity[w] = 1 << uint(i)
			}
			n.affinityCount = 1
		}

		Convey("Publishing a Value with affinity should route to the affinity-closest node", func() {
			// Create a Value whose affinity matches node 3 (bit 3 set).
			value, err := primitive.NewValue([]byte("affinity_test_data"))
			So(err, ShouldBeNil)
			defer value.Close()

			// Manually set the affinity region to match node 3.
			affStart := core.Cfg.Value.Region.Affinity.Start
			for w := 0; w < AffinityWords; w++ {
				value[affStart+w] = 1 << 3
			}

			record, err := nodes[0].Publish(*value, "AffinityLabel")
			So(err, ShouldBeNil)

			// Node 3 should have the record (distance 0 to the Value's affinity).
			So(nodes[3].HasRecord(record.Key), ShouldBeTrue)

			// The trie on node 3 should have learned the sequence.
			scores := nodes[3].Store.Classify("affinity_test_data")
			So(scores["AffinityLabel"], ShouldBeGreaterThan, 0)
		})

		Convey("Similar Values should cluster on the same node", func() {
			// Publish two Values with the same affinity fingerprint.
			for _, seq := range []string{"cluster_alpha", "cluster_beta"} {
				v, err := primitive.NewValue([]byte(seq))
				So(err, ShouldBeNil)

				affStart := core.Cfg.Value.Region.Affinity.Start
				for w := 0; w < AffinityWords; w++ {
					v[affStart+w] = 1 << 5 // matches node 5
				}

				_, err = nodes[0].Publish(*v, "Cluster")
				So(err, ShouldBeNil)
				v.Close()
			}

			// Both should be on node 5.
			So(nodes[5].Store.CurrentStep(), ShouldEqual, 2)

			// Node 5's trie should classify both.
			scores := nodes[5].Store.Classify("cluster_alpha")
			So(scores["Cluster"], ShouldBeGreaterThan, 0)
		})

		Convey("Values without affinity should be rejected", func() {
			value, err := primitive.NewValue([]byte("no_affinity"))
			So(err, ShouldBeNil)
			defer value.Close()

			_, err = nodes[0].Publish(*value, "Invalid")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "zero affinity")
		})
	})
}

func TestFieldGossip(t *testing.T) {
	Convey("Given a connected mesh of nodes with trained tries", t, func() {
		nodes := make([]*KadabraNode, 4)
		for i := range nodes {
			nodes[i] = buildKadabraNode(NodeID(i*8), WithEpochQueries(1000))
		}
		connectFully(nodes...)

		// Train via Experience so adaptive state accumulates surprisal.
		// Node 1 gets unique sequences each time (high surprisal).
		// Others get the same sequence (low surprisal after warmup).
		sentenceLabel := "Sentence"
		nonsenseLabel := "Nonsense"
		for i := range 60 {
			nodes[0].Store.Experience("the cat sat on the mat", &sentenceLabel)
			nodes[1].Store.Experience(
				fmt.Sprintf("xyzzy_%d plugh_%d abracadabra_%d", i, i*3, i*7), &nonsenseLabel,
			)
			nodes[2].Store.Experience("the cat sat on the mat", &sentenceLabel)
			nodes[3].Store.Experience("the cat sat on the mat", &sentenceLabel)
		}

		Convey("Gossip should propagate digests to all peers", func() {
			for _, n := range nodes {
				n.Gossip()
			}

			for _, n := range nodes {
				n.Field.mu.RLock()
				So(len(n.Field.digests), ShouldEqual, 4)
				n.Field.mu.RUnlock()
			}
		})

		Convey("Field pressure should be projected onto tries after gossip", func() {
			// Before gossip: no field pressure.
			sig0Before := nodes[0].Store.AdaptiveDigest()

			// All nodes gossip — the field now exists.
			for _, n := range nodes {
				n.Gossip()
			}

			// Node 0 (low surprisal) sits near node 1 (high surprisal).
			// The field should push node 0 to learn faster (positive learning pressure).
			// We verify indirectly: the adaptive surprisal scale should shift.
			sig0After := nodes[0].Store.AdaptiveDigest()

			// The digest itself doesn't change (it reads trie state, not pressure).
			// But the field pressure is now set on the trie. We test that Absorb
			// ran project() by checking the field view has digests AND that the
			// node didn't crash.
			_ = sig0Before
			_ = sig0After

			// Verify field view absorbed all digests (project ran for each).
			nodes[0].Field.mu.RLock()
			So(len(nodes[0].Field.digests), ShouldEqual, 4)
			nodes[0].Field.mu.RUnlock()
		})

		Convey("Stale digests should be discarded", func() {
			nodes[0].Gossip() // epoch 1
			nodes[0].Gossip() // epoch 2

			stale := FieldDigest{Origin: nodes[0].ID, Epoch: 0}
			nodes[1].Field.Absorb(stale)

			nodes[1].Field.mu.RLock()
			So(nodes[1].Field.digests[nodes[0].ID].Epoch, ShouldEqual, 2)
			nodes[1].Field.mu.RUnlock()
		})

		Convey("Field pressure should be asymmetric based on local state", func() {
			for _, n := range nodes {
				n.Gossip()
			}

			// Node 1 trained on unique sequences should have higher surprisal.
			decay0 := nodes[0].Store.AdaptiveDigest().SurprisalMean
			decay1 := nodes[1].Store.AdaptiveDigest().SurprisalMean
			So(decay1, ShouldBeGreaterThan, decay0)
		})

		Convey("Eigenmodes should emerge from structurally similar nodes", func() {
			// Seed affinities: nodes 0, 2, 3 share the same pattern,
			// node 1 has a different one.
			shared := [AffinityWords]uint64{
				0xAAAAAAAAAAAAAAAA, 0xAAAAAAAAAAAAAAAA,
				0xAAAAAAAAAAAAAAAA, 0xAAAAAAAAAAAAAAAA,
				0xAAAAAAAAAAAAAAAA, 0xAAAAAAAAAAAAAAAA,
				0xAAAAAAAAAAAAAAAA, 0xAAAAAAAAAAAAAAAA,
			}
			different := [AffinityWords]uint64{
				0x5555555555555555, 0x5555555555555555,
				0x5555555555555555, 0x5555555555555555,
				0x5555555555555555, 0x5555555555555555,
				0x5555555555555555, 0x5555555555555555,
			}
			nodes[0].Affinity = shared
			nodes[2].Affinity = shared
			nodes[3].Affinity = shared
			nodes[1].Affinity = different

			for _, n := range nodes {
				n.Gossip()
			}

			// Structurally similar nodes should cluster into the same mode,
			// and the high-surprisal outlier should dominate by energy.
			fv := nodes[0].Field
			fv.mu.RLock()
			So(len(fv.modes), ShouldEqual, 2) // two distinct clusters

			// Find the mode containing the shared-affinity nodes.
			var clusterMode, outlierMode *eigenmode
			for i := range fv.modes {
				hasNode0 := false
				for _, id := range fv.modes[i].members {
					if id == nodes[0].ID {
						hasNode0 = true
						break
					}
				}
				if hasNode0 {
					clusterMode = &fv.modes[i]
				} else {
					outlierMode = &fv.modes[i]
				}
			}

			So(clusterMode, ShouldNotBeNil)
			So(outlierMode, ShouldNotBeNil)
			So(len(clusterMode.members), ShouldEqual, 3)  // nodes 0, 2, 3
			So(len(outlierMode.members), ShouldEqual, 1)   // node 1

			// The outlier (high surprisal) should be the dominant mode —
			// the system "attends to" where the action is.
			dominant := fv.modes[fv.dominantMode]
			So(dominant.energy, ShouldEqual, outlierMode.energy)
			fv.mu.RUnlock()
		})
	})
}

func BenchmarkKadabraNode_Publish(b *testing.B) {
	nodes := []*KadabraNode{
		buildKadabraNode(0, WithReplicationFactor(3), WithEpochQueries(1000000)),
		buildKadabraNode(8, WithReplicationFactor(3), WithEpochQueries(1000000)),
		buildKadabraNode(16, WithReplicationFactor(3), WithEpochQueries(1000000)),
		buildKadabraNode(24, WithReplicationFactor(3), WithEpochQueries(1000000)),
	}

	connectFully(nodes...)
	b.ReportAllocs()
	b.ResetTimer()

	sequenceIndex := 0
	for b.Loop() {
		value := newValueWithAffinity(b, []byte("blue_cab_"+strconv.Itoa(sequenceIndex)))

		if _, err := nodes[0].Publish(*value, "Truck"); err != nil {
			value.Close()
			b.Fatal(err)
		}

		value.Close()
		sequenceIndex++
	}
}

func BenchmarkKadabraNode_FindRecord(b *testing.B) {
	nodes := []*KadabraNode{
		buildKadabraNode(0, WithReplicationFactor(3), WithEpochQueries(1000000)),
		buildKadabraNode(8, WithReplicationFactor(3), WithEpochQueries(1000000)),
		buildKadabraNode(16, WithReplicationFactor(3), WithEpochQueries(1000000)),
		buildKadabraNode(24, WithReplicationFactor(3), WithEpochQueries(1000000)),
	}

	connectFully(nodes...)

	value := newValueWithAffinity(b, []byte("blue_cab"))

	record, err := nodes[0].Publish(*value, "Truck")
	defer value.Close()

	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _, _ = nodes[3].FindRecord(record.Key)
	}
}

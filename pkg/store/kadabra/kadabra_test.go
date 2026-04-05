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
	"github.com/theapemachine/six/pkg/store/frankentrie"
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
		WithLocalStore(frankentrie.NewStore(
			frankentrie.WithDecayFactor(1),
			frankentrie.WithRandomSource(rand.NewSource(int64(id)+11)),
		)),
		WithNodeRandomSource(rand.NewSource(int64(id) + 23)),
	}

	nodeOptions = append(nodeOptions, options...)

	return NewKadabraNode(id, nodeOptions...)
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

		Convey("Publish should replicate onto the closest nodes to the record key", func() {
			value, err := primitive.NewValue([]byte("blue_cab"))
			So(err, ShouldBeNil)
			defer value.Close()

			record, err := nodes[0].Publish(*value, "Truck")

			So(err, ShouldBeNil)
			expected := expectedClosestNodeIDs(nodes, NodeID(record.Key), 2)

			for _, node := range nodes {
				shouldStore := false

				for _, expectedID := range expected {
					if node.ID == expectedID {
						shouldStore = true
						break
					}
				}

				So(node.HasRecord(record.Key), ShouldEqual, shouldStore)
			}
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

		value, err := primitive.NewValue([]byte("blue_cab"))
		So(err, ShouldBeNil)

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
			So(len(trace.Nodes), ShouldBeGreaterThan, 1)
			So(trace.Latency, ShouldBeGreaterThan, 0)
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
		value, err := primitive.NewValue([]byte("blue_cab_" + strconv.Itoa(sequenceIndex)))
		if err != nil {
			b.Fatal(err)
		}

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

	value, err := primitive.NewValue([]byte("blue_cab"))
	if err != nil {
		b.Fatal(err)
	}

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

package dmt

import (
	"net"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewNetworkNode(t *testing.T) {
	Convey("Given network configuration", t, func() {
		config := NetworkConfig{
			ListenAddr:   "127.0.0.1:0", // Random port
			NodeID:       "test-node",
			SyncInterval: time.Second,
		}

		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		Convey("When creating a new network node", func() {
			node, err := NewNetworkNode(config, forest)
			So(err, ShouldBeNil)
			defer node.Close()

			Convey("Then it should be properly initialized", func() {
				So(node, ShouldNotBeNil)
				So(node.config, ShouldResemble, config)
				So(node.forest, ShouldEqual, forest)
				So(node.peers, ShouldNotBeNil)
				So(node.merkleTree, ShouldNotBeNil)
				So(node.metrics, ShouldNotBeNil)
				So(node.listener, ShouldNotBeNil)
			})

			Convey("And it should be listening on a port", func() {
				addr := node.listener.Addr().(*net.TCPAddr)
				So(addr.Port, ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestPeerConnections(t *testing.T) {
	Convey("Given two network nodes", t, func() {
		// Create first node
		forest1, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest1.Close()

		node1, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "node1",
			SyncInterval: time.Second,
		}, forest1)
		So(err, ShouldBeNil)
		defer node1.Close()

		addr1 := node1.listener.Addr().String()

		// Create second node that connects to first
		forest2, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest2.Close()

		node2, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "node2",
			PeerAddrs:    []string{addr1},
			SyncInterval: time.Second,
		}, forest2)
		So(err, ShouldBeNil)
		defer node2.Close()

		Convey("When waiting for connection establishment", func() {
			// Wait for connection to be established
			time.Sleep(2 * time.Second)

			Convey("Then peers should be connected", func() {
				node1.peersMutex.RLock()
				node2.peersMutex.RLock()
				defer node1.peersMutex.RUnlock()
				defer node2.peersMutex.RUnlock()

				// At least one node should see the other as peer
				totalPeers := len(node1.peers) + len(node2.peers)
				So(totalPeers, ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestNetworkSync(t *testing.T) {
	Convey("Given connected network nodes", t, func() {
		// Create first node
		forest1, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest1.Close()

		node1, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "node1",
			SyncInterval: 100 * time.Millisecond,
		}, forest1)
		So(err, ShouldBeNil)
		defer node1.Close()

		addr1 := node1.listener.Addr().String()

		// Create second node
		forest2, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest2.Close()

		node2, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "node2",
			PeerAddrs:    []string{addr1},
			SyncInterval: 100 * time.Millisecond,
		}, forest2)
		So(err, ShouldBeNil)
		defer node2.Close()

		Convey("When inserting data on first node", func() {
			node1.forest.Insert([]byte("key1"), []byte("value1"))
			node1.merkleTree.Insert([]byte("key1"), []byte("value1"))
			node1.merkleTree.Rebuild()

			deadline := time.Now().Add(2 * time.Second)
			for !node2.merkleTree.Verify([]byte("key1"), []byte("value1")) && time.Now().Before(deadline) {
				time.Sleep(25 * time.Millisecond)
			}

			Convey("Then data should be synced to second node", func() {
				exists := node2.merkleTree.Verify([]byte("key1"), []byte("value1"))
				So(exists, ShouldBeTrue)
			})
		})
	})
}

func TestNetworkInsert(t *testing.T) {
	Convey("Given a network node", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "test-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		Convey("When inserting data directly", func() {
			node.merkleTree.Insert([]byte("key"), []byte("value"))
			node.merkleTree.Rebuild()

			Convey("Then it should be verifiable", func() {
				exists := node.merkleTree.Verify([]byte("key"), []byte("value"))
				So(exists, ShouldBeTrue)
			})
		})
	})
}

func TestNetworkMetrics(t *testing.T) {
	Convey("Given a network node", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "test-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		Convey("When getting metrics", func() {
			metrics := node.GetMetrics()

			Convey("Then metrics should be available", func() {
				So(metrics, ShouldNotBeNil)
				So(metrics["network"], ShouldNotBeNil)
				networkStats := metrics["network"].(map[string]interface{})
				So(networkStats["peer_count"], ShouldNotBeNil)
			})
		})
	})
}

func TestNetworkClose(t *testing.T) {
	Convey("Given a network node with peers", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "test-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)

		Convey("When closing the node", func() {
			err := node.Close()

			Convey("Then it should close successfully", func() {
				So(err, ShouldBeNil)

				// Verify listener is closed
				_, err := net.Dial("tcp", node.listener.Addr().String())
				So(err, ShouldNotBeNil)

				// Verify context is cancelled
				select {
				case <-node.ctx.Done():
					So(true, ShouldBeTrue)
				default:
					So(false, ShouldBeTrue, "Context not cancelled")
				}
			})
		})
	})
}

func TestNetworkBroadcast(t *testing.T) {
	Convey("Given a network node with peers", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "test-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		Convey("When broadcasting an insert", func() {
			node.election.stateLock.Lock()
			node.election.term = 1
			node.election.stateLock.Unlock()

			err := node.BroadcastInsert([]byte("broadcast-key"), []byte("broadcast-value"))
			So(err, ShouldBeNil)

			Convey("Then metrics should be updated", func() {
				metrics := node.GetMetrics()
				networkStats := metrics["network"].(map[string]interface{})
				So(networkStats["bytes_tx"], ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestNetworkQuorumSizing(t *testing.T) {
	Convey("Given a network config with multiple peers", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr: "127.0.0.1:0",
			NodeID:     "quorum-node",
			PeerAddrs: []string{
				"127.0.0.1:1111",
				"127.0.0.1:2222",
				"127.0.0.1:3333",
				"127.0.0.1:4444",
			},
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		Convey("It should derive quorum as strict majority", func() {
			// 5 nodes in cluster (self + 4 peers) => quorum 3
			So(node.election.config.QuorumSize, ShouldEqual, 3)
		})
	})
}

/*
TestNetworkUpdateMerkleRoot checks that rebuilding the merkle view from the fastest
tree produces a non-nil root and leaves that Verify accepts after forest inserts.
*/
func TestNetworkUpdateMerkleRoot(t *testing.T) {
	Convey("Given a network node and forest data", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "merkle-root-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		key := []byte("merkle-net-key")
		value := []byte("merkle-net-value")

		forest.Insert(key, value)

		Convey("When updateMerkleRoot runs", func() {
			node.updateMerkleRoot()

			Convey("Then the merkle root should exist and verify the pair", func() {
				So(node.merkleTree.Root, ShouldNotBeNil)
				So(node.merkleTree.Verify(key, value), ShouldBeTrue)
			})
		})
	})
}

/*
TestNetworkMetricsAfterInsert asserts BroadcastInsert drives both insert counter
and bytes_tx in the metrics snapshot.
*/
func TestNetworkMetricsAfterInsert(t *testing.T) {
	Convey("Given a network node", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "metrics-insert-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		Convey("When broadcasting an insert", func() {
			node.election.stateLock.Lock()
			node.election.term = 1
			node.election.stateLock.Unlock()

			err := node.BroadcastInsert([]byte("m-key"), []byte("m-val"))
			So(err, ShouldBeNil)

			Convey("Then operations and network metrics reflect the write", func() {
				metrics := node.GetMetrics()
				So(metrics, ShouldNotBeNil)

				networkStats := metrics["network"].(map[string]interface{})
				So(networkStats["bytes_tx"], ShouldBeGreaterThan, 0)

				ops := metrics["operations"].(map[string]uint64)
				So(ops["insert"], ShouldBeGreaterThan, 0)
			})
		})
	})
}

/*
TestNetworkTwoNodeBroadcast uses symmetric PeerAddrs so each node registers an
outbound RPC client; broadcast originates from node1, which only sends to peers
it holds in its map (outbound connections).
*/
func TestNetworkTwoNodeBroadcast(t *testing.T) {
	Convey("Given two nodes with mutual peer addresses", t, func() {
		forest1, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest1.Close()

		node1, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "broadcast-a",
			SyncInterval: 100 * time.Millisecond,
		}, forest1)
		So(err, ShouldBeNil)
		defer node1.Close()

		addr1 := node1.listener.Addr().String()

		forest2, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest2.Close()

		node2, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "broadcast-b",
			PeerAddrs:    []string{addr1},
			SyncInterval: 100 * time.Millisecond,
		}, forest2)
		So(err, ShouldBeNil)
		defer node2.Close()

		addr2 := node2.listener.Addr().String()
		node1.config.PeerAddrs = []string{addr2}

		bcastKey := []byte("two-node-bcast-key")
		bcastVal := []byte("two-node-bcast-val")

		Convey("When peers connect and node1 broadcasts an insert", func() {
			deadline := time.Now().Add(3 * time.Second)

			for time.Now().Before(deadline) {
				node1.peersMutex.RLock()
				n1 := len(node1.peers)
				node1.peersMutex.RUnlock()

				node2.peersMutex.RLock()
				n2 := len(node2.peers)
				node2.peersMutex.RUnlock()

				if n1 > 0 && n2 > 0 {
					break
				}

				time.Sleep(15 * time.Millisecond)
			}

			node1.election.stateLock.Lock()
			node1.election.term = 1
			node1.election.stateLock.Unlock()

			err := node1.BroadcastInsert(bcastKey, bcastVal)
			So(err, ShouldBeNil)

			received := false

			for time.Now().Before(deadline) {
				if v, ok := forest2.Get(bcastKey); ok && string(v) == string(bcastVal) {
					received = true

					break
				}

				if node2.merkleTree.Verify(bcastKey, bcastVal) {
					received = true

					break
				}

				time.Sleep(15 * time.Millisecond)
			}

			Convey("Then node2 should hold the inserted data", func() {
				So(received, ShouldBeTrue)
			})
		})
	})
}

func TestNetworkBroadcastInsertRejectsZeroTerm(t *testing.T) {
	Convey("Given a network node with an invalid election term", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "broadcast-zero-term",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		node.election.stateLock.Lock()
		node.election.term = 0
		node.election.stateLock.Unlock()

		Convey("BroadcastInsert should reject the invalid state", func() {
			err := node.BroadcastInsert([]byte("bad-key"), []byte("bad-value"))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldEqual, "dmt/network broadcast insert: missing term in request")
		})
	})
}

/*
TestNetworkNodeElectionSetup checks quorum size for a five-node cluster
(four configured peers plus self): majority is three.
*/
func TestNetworkNodeElectionSetup(t *testing.T) {
	Convey("Given a node with four peer addresses", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr: "127.0.0.1:0",
			NodeID:     "election-setup-node",
			PeerAddrs: []string{
				"127.0.0.1:60001",
				"127.0.0.1:60002",
				"127.0.0.1:60003",
				"127.0.0.1:60004",
			},
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		Convey("Then quorum size should be strict majority of five nodes", func() {
			// (4 peers + 1 self) => cluster 5 => (5/2)+1 == 3
			So(node.election.config.QuorumSize, ShouldEqual, 3)
		})
	})
}

/*
TestNetworkCloseIdempotent ensures a second Close does not panic; election shutdown
is already guarded by sync.Once.
*/
func TestNetworkCloseIdempotent(t *testing.T) {
	Convey("Given a closed network node", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "close-twice-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)

		So(node.Close(), ShouldBeNil)

		Convey("When Close is invoked again", func() {
			So(func() { _ = node.Close() }, ShouldNotPanic)
		})
	})
}

/*
TestNetworkGetMetricsStructure checks the top-level keys exposed by GetMetrics.
*/
func TestNetworkGetMetricsStructure(t *testing.T) {
	Convey("Given a network node", t, func() {
		forest, err := NewForest(ForestConfig{})
		So(err, ShouldBeNil)
		defer forest.Close()

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "metrics-shape-node",
			SyncInterval: time.Second,
		}, forest)
		So(err, ShouldBeNil)
		defer node.Close()

		Convey("When GetMetrics is called", func() {
			metrics := node.GetMetrics()

			Convey("Then expected sections should be present", func() {
				So(metrics, ShouldNotBeNil)
				_, okNet := metrics["network"]
				_, okOps := metrics["operations"]
				_, okElect := metrics["election"]
				_, okLat := metrics["latencies"]
				_, okNode := metrics["node"]
				So(okNet, ShouldBeTrue)
				So(okOps, ShouldBeTrue)
				So(okElect, ShouldBeTrue)
				So(okLat, ShouldBeTrue)
				So(okNode, ShouldBeTrue)
			})
		})
	})
}

/*
BenchmarkNetworkNodeCreate measures allocation and time to stand up and tear down
a node on an ephemeral port.
*/
func BenchmarkNetworkNodeCreate(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		forest, err := NewForest(ForestConfig{})
		if err != nil {
			b.Fatal(err)
		}

		node, err := NewNetworkNode(NetworkConfig{
			ListenAddr:   "127.0.0.1:0",
			NodeID:       "bench-create",
			SyncInterval: time.Second,
		}, forest)
		if err != nil {
			forest.Close()
			b.Fatal(err)
		}

		if closeErr := node.Close(); closeErr != nil {
			forest.Close()
			b.Fatal(closeErr)
		}

		if closeErr := forest.Close(); closeErr != nil {
			b.Fatal(closeErr)
		}
	}
}

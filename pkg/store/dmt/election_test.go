package dmt

import (
	"context"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func newTestElectionNode(nodeID string) (*NetworkNode, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	forest, err := NewForest(ForestConfig{})
	if err != nil {
		panic(err)
	}

	node := &NetworkNode{
		config:  NetworkConfig{NodeID: nodeID},
		metrics: NewMetrics(),
		ctx:     ctx,
		cancel:  cancel,
		forest:  forest,
		peers:   make(map[string]*peer),
	}

	return node, func() {
		cancel()
		forest.Close()
	}
}

func TestNewElection(t *testing.T) {
	Convey("Given election configuration", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		Convey("When creating a new election manager", func() {
			election := NewElection(config, node)
			defer election.Close()

			Convey("Then it should be properly initialized", func() {
				So(election, ShouldNotBeNil)
				So(election.config, ShouldResemble, config)
				So(election.node, ShouldEqual, node)
				So(election.role, ShouldEqual, Follower)
				So(election.term, ShouldEqual, 0)
				So(election.votedFor, ShouldEqual, "")
			})
		})
	})
}

func TestElectionStateTransitions(t *testing.T) {
	Convey("Given an election manager", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   100 * time.Millisecond,
			HeartbeatInterval: 50 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		Convey("When stepping down", func() {
			election.stepDown(5)

			Convey("Then state should be updated", func() {
				So(election.getState(), ShouldEqual, Follower)
				So(election.getCurrentTerm(), ShouldEqual, uint64(5))
				So(election.votedFor, ShouldEqual, "")
			})
		})

		Convey("When becoming leader", func() {
			election.becomeLeader()

			Convey("Then state should be updated", func() {
				So(election.getState(), ShouldEqual, Leader)
				So(node.metrics.isLeader.Load(), ShouldBeTrue)
			})
		})
	})
}

func TestVoteHandling(t *testing.T) {
	Convey("Given an election manager", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		Convey("When handling vote requests", func() {
			Convey("With higher term", func() {
				granted := election.handleVoteRequest(2, "candidate1", 1, 1)
				So(granted, ShouldBeTrue)
				So(election.getCurrentTerm(), ShouldEqual, uint64(2))
				So(election.votedFor, ShouldEqual, "candidate1")
			})

			Convey("With lower term", func() {
				election.term = 5
				granted := election.handleVoteRequest(3, "candidate2", 1, 1)
				So(granted, ShouldBeFalse)
			})

			Convey("With already voted in term", func() {
				election.term = 5
				election.votedFor = "candidate1"
				granted := election.handleVoteRequest(5, "candidate2", 1, 1)
				So(granted, ShouldBeFalse)
			})

			Convey("With outdated log", func() {
				election.logLock.Lock()
				election.lastLogIndex = 10
				election.lastLogTerm = 5
				election.logLock.Unlock()

				granted := election.handleVoteRequest(6, "candidate1", 5, 4)
				So(granted, ShouldBeFalse)
			})
		})
	})
}

func TestHeartbeatHandling(t *testing.T) {
	Convey("Given an election manager", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		Convey("When handling heartbeats", func() {
			Convey("With higher term", func() {
				success := election.handleHeartbeat(2, "leader1")
				So(success, ShouldBeTrue)
				So(election.getCurrentTerm(), ShouldEqual, uint64(2))
				So(election.getState(), ShouldEqual, Follower)
			})

			Convey("With lower term", func() {
				election.term = 5
				success := election.handleHeartbeat(3, "leader1")
				So(success, ShouldBeFalse)
			})

			Convey("With same term", func() {
				election.term = 5
				success := election.handleHeartbeat(5, "leader1")
				So(success, ShouldBeTrue)
			})
		})
	})
}

func TestLogStateManagement(t *testing.T) {
	Convey("Given an election manager", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		Convey("When updating log state", func() {
			election.updateLogState(5, 2)

			Convey("Then log indices should be updated", func() {
				So(election.getLastLogIndex(), ShouldEqual, uint64(5))
				lastTerm := election.lastLogTerm
				So(lastTerm, ShouldEqual, uint64(2))
			})

			Convey("When updating with lower index", func() {
				election.updateLogState(3, 1)
				So(election.getLastLogIndex(), ShouldEqual, uint64(5))
				lastTerm := election.lastLogTerm
				So(lastTerm, ShouldEqual, uint64(2))
			})

			Convey("When updating with higher index", func() {
				election.updateLogState(7, 3)
				So(election.getLastLogIndex(), ShouldEqual, uint64(7))
				lastTerm := election.lastLogTerm
				So(lastTerm, ShouldEqual, uint64(3))
			})
		})
	})
}

func TestElectionTimeout(t *testing.T) {
	Convey("Given an election manager with short timeout", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   50 * time.Millisecond,
			HeartbeatInterval: 10 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		Convey("When waiting for timeout", func() {
			// Wait for potential state change
			time.Sleep(100 * time.Millisecond)

			Convey("Then state should change to candidate", func() {
				state := election.getState()
				So(state, ShouldEqual, Candidate)
				So(election.getCurrentTerm(), ShouldBeGreaterThan, 0)
			})
		})
	})
}

/*
TestElectionDoubleClose verifies repeated Close uses sync.Once and does not panic.
*/
func TestElectionDoubleClose(t *testing.T) {
	Convey("Given an election manager", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		Convey("When Close is invoked twice", func() {
			election.Close()

			So(func() { election.Close() }, ShouldNotPanic)
		})
	})
}

/*
TestElectionStepDownResetsVote checks stepDown clears votedFor and returns to Follower.
*/
func TestElectionStepDownResetsVote(t *testing.T) {
	Convey("Given an election with a prior vote", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		election.stateLock.Lock()
		election.votedFor = "X"
		election.stateLock.Unlock()

		Convey("When stepping down to a new term", func() {
			election.stepDown(10)

			Convey("Then votedFor is cleared and role is Follower", func() {
				So(election.getState(), ShouldEqual, Follower)
				So(election.getCurrentTerm(), ShouldEqual, uint64(10))
				So(election.votedFor, ShouldEqual, "")
			})
		})
	})
}

/*
TestElectionHandleVoteRequestSameCandidate ensures a second request from the same
candidate in the same term is still granted (idempotent grant).
*/
func TestElectionHandleVoteRequestSameCandidate(t *testing.T) {
	Convey("Given an election in term 5 that voted for candidate1", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		election.stateLock.Lock()
		election.term = 5
		election.stateLock.Unlock()

		first := election.handleVoteRequest(5, "candidate1", 10, 5)
		So(first, ShouldBeTrue)

		Convey("When the same candidate requests again in the same term", func() {
			second := election.handleVoteRequest(5, "candidate1", 10, 5)

			Convey("Then the vote should still be granted", func() {
				So(second, ShouldBeTrue)
				So(election.votedFor, ShouldEqual, "candidate1")
				So(election.getCurrentTerm(), ShouldEqual, uint64(5))
			})
		})
	})
}

/*
TestElectionHandleVoteRequestLogComparison exercises Raft log-up-to-date checks.
*/
func TestElectionHandleVoteRequestLogComparison(t *testing.T) {
	Convey("Given local log at term 5 index 10", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		election.logLock.Lock()
		election.lastLogTerm = 5
		election.lastLogIndex = 10
		election.logLock.Unlock()

		election.stateLock.Lock()
		election.term = 5
		election.stateLock.Unlock()

		Convey("When candidate log is behind on index at same last term", func() {
			denied := election.handleVoteRequest(5, "lagging", 9, 5)

			Convey("Then the vote is denied", func() {
				So(denied, ShouldBeFalse)
			})
		})

		Convey("When candidate lastLogTerm is higher", func() {
			granted := election.handleVoteRequest(5, "fresh", 9, 6)

			Convey("Then the vote is granted", func() {
				So(granted, ShouldBeTrue)
				So(election.votedFor, ShouldEqual, "fresh")
			})
		})
	})
}

/*
TestElectionHandleHeartbeatFromLeaderAsCurrent verifies same-term heartbeats are
rejected while this node is Leader.
*/
func TestElectionHandleHeartbeatFromLeaderAsCurrent(t *testing.T) {
	Convey("Given a node that is Leader in term 5", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		election.stateLock.Lock()
		election.role = Leader
		election.term = 5
		election.stateLock.Unlock()

		Convey("When a heartbeat arrives at the same term", func() {
			ok := election.handleHeartbeat(5, "peer-leader")

			Convey("Then it should be rejected", func() {
				So(ok, ShouldBeFalse)
				So(election.getState(), ShouldEqual, Leader)
			})
		})
	})
}

/*
TestElectionHandleVoteNotCandidate checks handleVote does not count votes when not Candidate.

The implementation returns before RecordVote; metrics votes_received must stay unchanged.
*/
func TestElectionHandleVoteNotCandidate(t *testing.T) {
	Convey("Given a Follower election", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		election.stateLock.Lock()
		election.role = Follower
		election.stateLock.Unlock()

		before := node.metrics.GetMetrics()
		electionMap := before["election"].(map[string]interface{})
		votesBefore := electionMap["votes_received"].(uint64)

		Convey("When handleVote runs", func() {
			election.handleVote("peer1")

			after := node.metrics.GetMetrics()
			electionAfter := after["election"].(map[string]interface{})
			votesAfter := electionAfter["votes_received"].(uint64)

			Convey("Then role stays Follower and vote is not recorded", func() {
				So(election.getState(), ShouldEqual, Follower)
				So(votesAfter, ShouldEqual, votesBefore)
			})
		})
	})
}

/*
TestElectionUpdateLogStateMonotonic asserts index and term only move forward.
*/
func TestElectionUpdateLogStateMonotonic(t *testing.T) {
	Convey("Given an election manager", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		election.updateLogState(5, 2)

		Convey("When a lower index update is applied", func() {
			election.updateLogState(3, 1)

			Convey("Then last index and term stay at the prior high water mark", func() {
				So(election.getLastLogIndex(), ShouldEqual, uint64(5))
				So(election.getLastLogTerm(), ShouldEqual, uint64(2))
			})
		})

		Convey("When a higher index update is applied", func() {
			election.updateLogState(7, 3)

			Convey("Then log state advances", func() {
				So(election.getLastLogIndex(), ShouldEqual, uint64(7))
				So(election.getLastLogTerm(), ShouldEqual, uint64(3))
			})
		})
	})
}

/*
TestElectionConcurrentStateAccess stress-reads election fields alongside mutations.
*/
func TestElectionConcurrentStateAccess(t *testing.T) {
	Convey("Given an election under concurrent access", t, func() {
		config := ElectionConfig{
			ElectionTimeout:   time.Second,
			HeartbeatInterval: 100 * time.Millisecond,
			QuorumSize:        2,
		}

		node, cleanup := newTestElectionNode("test-node")
		defer cleanup()

		election := NewElection(config, node)
		defer election.Close()

		var wg sync.WaitGroup
		iters := 500

		for range 10 {
			wg.Add(1)

			go func() {
				defer wg.Done()

				for range iters {
					_ = election.getState()
					_ = election.getCurrentTerm()
					_ = election.getLastLogIndex()
				}
			}()
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range iters {
				if i%2 == 0 {
					election.stepDown(uint64(100 + i))
				} else {
					election.updateLogState(uint64(i), uint64(i/2))
				}
			}
		}()

		Convey("When goroutines complete", func() {
			wg.Wait()

			Convey("Then accessors still return coherent values", func() {
				state := election.getState()
				So(state == Follower || state == Candidate || state == Leader, ShouldBeTrue)
				So(election.getCurrentTerm(), ShouldBeGreaterThan, uint64(0))
			})
		})
	})
}

/*
BenchmarkElectionGetState measures hot-path read lock for role.
*/
func BenchmarkElectionGetState(b *testing.B) {
	config := ElectionConfig{
		ElectionTimeout:   time.Second,
		HeartbeatInterval: 100 * time.Millisecond,
		QuorumSize:        2,
	}

	node, cleanup := newTestElectionNode("bench-node")
	defer cleanup()

	election := NewElection(config, node)
	b.Cleanup(func() { election.Close() })

	b.ReportAllocs()

	for b.Loop() {
		_ = election.getState()
	}
}

/*
BenchmarkElectionHandleVoteRequest measures vote RPC handling with monotonically
increasing terms each iteration.
*/
func BenchmarkElectionHandleVoteRequest(b *testing.B) {
	config := ElectionConfig{
		ElectionTimeout:   time.Second,
		HeartbeatInterval: 100 * time.Millisecond,
		QuorumSize:        2,
	}

	node, cleanup := newTestElectionNode("bench-node")
	defer cleanup()

	election := NewElection(config, node)
	b.Cleanup(func() { election.Close() })

	var term uint64

	b.ReportAllocs()

	for b.Loop() {
		term++
		_ = election.handleVoteRequest(term, "candidate", 0, 0)
	}
}

/*
BenchmarkElectionHandleHeartbeat measures heartbeat handling on the follower path.
*/
func BenchmarkElectionHandleHeartbeat(b *testing.B) {
	config := ElectionConfig{
		ElectionTimeout:   time.Second,
		HeartbeatInterval: 100 * time.Millisecond,
		QuorumSize:        2,
	}

	node, cleanup := newTestElectionNode("bench-node")
	defer cleanup()

	election := NewElection(config, node)
	b.Cleanup(func() { election.Close() })

	election.stateLock.Lock()
	election.role = Follower
	election.term = 1
	election.stateLock.Unlock()

	b.ReportAllocs()

	for b.Loop() {
		_ = election.handleHeartbeat(1, "leader")
	}
}

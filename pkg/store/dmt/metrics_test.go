package dmt

import (
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewMetrics(t *testing.T) {
	Convey("Given a new metrics instance", t, func() {
		metrics := NewMetrics()

		Convey("Then it should be properly initialized", func() {
			So(metrics, ShouldNotBeNil)
			So(metrics.insertLatency, ShouldNotBeNil)
			So(metrics.lookupLatency, ShouldNotBeNil)
			So(metrics.syncLatency, ShouldNotBeNil)
			So(metrics.networkLatency, ShouldNotBeNil)
		})

		Convey("And counters should be initialized to zero", func() {
			So(metrics.insertCount.Load(), ShouldEqual, 0)
			So(metrics.lookupCount.Load(), ShouldEqual, 0)
			So(metrics.syncCount.Load(), ShouldEqual, 0)
			So(metrics.conflictCount.Load(), ShouldEqual, 0)
		})
	})
}

func TestLatencyTracker(t *testing.T) {
	Convey("Given a new latency tracker", t, func() {
		tracker := NewLatencyTracker(100)

		Convey("When recording latencies", func() {
			tracker.RecordLatency(10 * time.Millisecond)
			tracker.RecordLatency(20 * time.Millisecond)
			tracker.RecordLatency(30 * time.Millisecond)

			Convey("Then average should be calculated correctly", func() {
				avg := tracker.AverageLatency()
				So(avg, ShouldEqual, 20*time.Millisecond)
			})
		})

		Convey("When no latencies are recorded", func() {
			avg := tracker.AverageLatency()
			So(avg, ShouldEqual, 0)
		})

		Convey("When window wraps around", func() {
			// Fill window
			for i := 0; i < 100; i++ {
				tracker.RecordLatency(10 * time.Millisecond)
			}
			// Add one more to wrap
			tracker.RecordLatency(20 * time.Millisecond)

			avg := tracker.AverageLatency()
			So(avg > 10*time.Millisecond, ShouldBeTrue)
			So(avg < 20*time.Millisecond, ShouldBeTrue)
		})
	})
}

func TestMetricsRecording(t *testing.T) {
	Convey("Given a metrics instance", t, func() {
		metrics := NewMetrics()

		Convey("When recording insert operations", func() {
			metrics.RecordInsert(10*time.Millisecond, 100)

			Convey("Then counters should be updated", func() {
				So(metrics.insertCount.Load(), ShouldEqual, 1)
				So(metrics.bytesTransmitted.Load(), ShouldEqual, 100)
				So(metrics.insertLatency.AverageLatency(), ShouldEqual, 10*time.Millisecond)
			})
		})

		Convey("When recording lookup operations", func() {
			metrics.RecordLookup(5 * time.Millisecond)

			Convey("Then counters should be updated", func() {
				So(metrics.lookupCount.Load(), ShouldEqual, 1)
				So(metrics.lookupLatency.AverageLatency(), ShouldEqual, 5*time.Millisecond)
			})
		})

		Convey("When recording sync operations", func() {
			metrics.RecordSync(15*time.Millisecond, 200)

			Convey("Then counters should be updated", func() {
				So(metrics.syncCount.Load(), ShouldEqual, 1)
				So(metrics.bytesReceived.Load(), ShouldEqual, 200)
				So(metrics.syncLatency.AverageLatency(), ShouldEqual, 15*time.Millisecond)
			})
		})

		Convey("When recording conflicts", func() {
			metrics.RecordConflict()

			Convey("Then conflict counter should be updated", func() {
				So(metrics.conflictCount.Load(), ShouldEqual, 1)
			})
		})
	})
}

func TestMetricsNetworkStats(t *testing.T) {
	Convey("Given a metrics instance", t, func() {
		metrics := NewMetrics()

		Convey("When updating peer count", func() {
			metrics.UpdatePeerCount(5)

			Convey("Then peer count should be updated", func() {
				So(metrics.peerCount.Load(), ShouldEqual, 5)
			})
		})

		Convey("When setting node role", func() {
			metrics.SetNodeRole("leader", 1.0)

			Convey("Then role should be updated", func() {
				So(metrics.nodeRole, ShouldEqual, "leader")
				So(metrics.nodeWeight, ShouldEqual, 1.0)
			})
		})

		Convey("When setting leader status", func() {
			metrics.SetLeader(true)

			Convey("Then leader status should be updated", func() {
				So(metrics.isLeader.Load(), ShouldBeTrue)
			})
		})
	})
}

func TestMetricsElectionStats(t *testing.T) {
	Convey("Given a metrics instance", t, func() {
		metrics := NewMetrics()

		Convey("When recording votes", func() {
			metrics.RecordVote("node1")
			metrics.RecordVote("node2")

			Convey("Then vote counters should be updated", func() {
				So(metrics.votesReceived.Load(), ShouldEqual, 2)
				So(metrics.lastVoter, ShouldEqual, "node2")
			})
		})
	})
}

func TestMetricsSnapshot(t *testing.T) {
	Convey("Given a metrics instance with recorded data", t, func() {
		metrics := NewMetrics()

		// Record various metrics
		metrics.RecordInsert(10*time.Millisecond, 100)
		metrics.RecordLookup(5 * time.Millisecond)
		metrics.RecordSync(15*time.Millisecond, 200)
		metrics.RecordConflict()
		metrics.UpdatePeerCount(5)
		metrics.SetNodeRole("follower", 0.5)
		metrics.SetLeader(false)
		metrics.RecordVote("node1")

		Convey("When getting metrics snapshot", func() {
			snapshot := metrics.GetMetrics()

			Convey("Then all metrics should be included", func() {
				operations := snapshot["operations"].(map[string]uint64)
				So(operations["insert"], ShouldEqual, 1)
				So(operations["lookup"], ShouldEqual, 1)
				So(operations["sync"], ShouldEqual, 1)
				So(operations["conflict"], ShouldEqual, 1)

				network := snapshot["network"].(map[string]interface{})
				So(network["bytes_tx"], ShouldEqual, uint64(100))
				So(network["bytes_rx"], ShouldEqual, uint64(200))
				So(network["peer_count"], ShouldEqual, int32(5))

				node := snapshot["node"].(map[string]interface{})
				So(node["is_leader"], ShouldBeFalse)
				So(node["role"], ShouldEqual, "follower")
				So(node["weight"], ShouldEqual, 0.5)

				election := snapshot["election"].(map[string]interface{})
				So(election["votes_received"], ShouldEqual, uint64(1))
				So(election["last_voter"], ShouldEqual, "node1")
			})
		})
	})
}

/*
TestMetricsConcurrentRecording verifies insert counting stays correct under
concurrent RecordInsert calls from many goroutines.
*/
func TestMetricsConcurrentRecording(t *testing.T) {
	Convey("Given metrics shared across goroutines", t, func() {
		metrics := NewMetrics()
		goroutineCount := 10
		insertsPerGoroutine := 1000
		var waitGroup sync.WaitGroup

		Convey("When goroutines record inserts concurrently", func() {
			for index := 0; index < goroutineCount; index++ {
				waitGroup.Add(1)

				go func() {
					defer waitGroup.Done()

					for counter := 0; counter < insertsPerGoroutine; counter++ {
						metrics.RecordInsert(time.Microsecond, 1)
					}
				}()
			}

			waitGroup.Wait()

			Convey("Then insertCount should equal the total recorded inserts", func() {
				expectedCount := uint64(goroutineCount * insertsPerGoroutine)

				So(metrics.insertCount.Load(), ShouldEqual, expectedCount)
			})
		})
	})
}

/*
TestLatencyTrackerFullWindow checks rolling average behavior when the window
fills and then is completely overwritten by newer samples.
*/
func TestLatencyTrackerFullWindow(t *testing.T) {
	Convey("Given a latency tracker with a small window", t, func() {
		tracker := NewLatencyTracker(5)

		Convey("When the window fills with one duration then wraps with another", func() {
			for index := 0; index < 5; index++ {
				tracker.RecordLatency(10 * time.Millisecond)
			}

			firstAverage := tracker.AverageLatency()
			So(firstAverage, ShouldEqual, 10*time.Millisecond)

			for index := 0; index < 5; index++ {
				tracker.RecordLatency(20 * time.Millisecond)
			}

			Convey("Then the average should reflect only the latest window", func() {
				secondAverage := tracker.AverageLatency()

				So(secondAverage, ShouldEqual, 20*time.Millisecond)
			})
		})
	})
}

/*
TestLatencyTrackerSingleEntry verifies AverageLatency with a single non-zero
sample in a larger window.
*/
func TestLatencyTrackerSingleEntry(t *testing.T) {
	Convey("Given a latency tracker with a sparse first sample", t, func() {
		tracker := NewLatencyTracker(10)

		Convey("When only one latency is recorded", func() {
			tracker.RecordLatency(50 * time.Millisecond)

			Convey("Then the average should equal that single sample", func() {
				So(tracker.AverageLatency(), ShouldEqual, 50*time.Millisecond)
			})
		})
	})
}

/*
TestMetricsGetMetricsStructure asserts the snapshot map exposes the expected
top-level sections for dashboards and introspection.
*/
func TestMetricsGetMetricsStructure(t *testing.T) {
	Convey("Given a fresh metrics instance", t, func() {
		metrics := NewMetrics()

		Convey("When GetMetrics is called", func() {
			snapshot := metrics.GetMetrics()

			Convey("Then all top-level keys should be present", func() {
				_, hasOperations := snapshot["operations"]
				_, hasElection := snapshot["election"]
				_, hasLatencies := snapshot["latencies"]
				_, hasNetwork := snapshot["network"]
				_, hasNode := snapshot["node"]

				So(hasOperations, ShouldBeTrue)
				So(hasElection, ShouldBeTrue)
				So(hasLatencies, ShouldBeTrue)
				So(hasNetwork, ShouldBeTrue)
				So(hasNode, ShouldBeTrue)
			})
		})
	})
}

/*
TestMetricsMultipleInserts checks cumulative counters and byte totals across
many insert operations.
*/
func TestMetricsMultipleInserts(t *testing.T) {
	Convey("Given metrics and many insert operations", t, func() {
		metrics := NewMetrics()
		insertCount := 100
		var expectedBytes uint64

		Convey("When inserts use increasing byte counts", func() {
			for index := 1; index <= insertCount; index++ {
				metrics.RecordInsert(time.Microsecond, index)
				expectedBytes += uint64(index)
			}

			Convey("Then insert and byte counters should match the workload", func() {
				So(metrics.insertCount.Load(), ShouldEqual, uint64(insertCount))
				So(metrics.bytesTransmitted.Load(), ShouldEqual, expectedBytes)
			})
		})
	})
}

/*
TestMetricsSyncUpdatesTime ensures RecordSync moves lastSyncTime forward
relative to a captured start instant.
*/
func TestMetricsSyncUpdatesTime(t *testing.T) {
	Convey("Given metrics and a start time", t, func() {
		metrics := NewMetrics()
		startTime := time.Now()

		Convey("When a sync is recorded", func() {
			metrics.RecordSync(time.Millisecond, 1)
			snapshot := metrics.GetMetrics()
			node := snapshot["node"].(map[string]interface{})
			lastSyncTime := node["last_sync_time"].(time.Time)

			Convey("Then lastSyncTime should not be before the test start", func() {
				So(lastSyncTime.Before(startTime), ShouldBeFalse)
			})
		})
	})
}

/*
BenchmarkRecordInsert measures the cost of recording a single insert sample.
*/
func BenchmarkRecordInsert(b *testing.B) {
	metrics := NewMetrics()

	b.ReportAllocs()

	for b.Loop() {
		metrics.RecordInsert(time.Microsecond, 64)
	}
}

/*
BenchmarkRecordLookup measures the cost of recording a single lookup sample.
*/
func BenchmarkRecordLookup(b *testing.B) {
	metrics := NewMetrics()

	b.ReportAllocs()

	for b.Loop() {
		metrics.RecordLookup(time.Microsecond)
	}
}

/*
BenchmarkRecordSync measures the cost of recording a single sync sample.
*/
func BenchmarkRecordSync(b *testing.B) {
	metrics := NewMetrics()

	b.ReportAllocs()

	for b.Loop() {
		metrics.RecordSync(time.Microsecond, 64)
	}
}

/*
BenchmarkGetMetrics measures snapshot construction after the tracker has data.
*/
func BenchmarkGetMetrics(b *testing.B) {
	metrics := NewMetrics()

	for index := 0; index < 50; index++ {
		metrics.RecordInsert(time.Millisecond, index)
		metrics.RecordLookup(time.Millisecond)
		metrics.RecordSync(time.Millisecond, index)
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = metrics.GetMetrics()
	}
}

/*
BenchmarkLatencyRecord measures appending one sample to a rolling window.
*/
func BenchmarkLatencyRecord(b *testing.B) {
	tracker := NewLatencyTracker(256)

	b.ReportAllocs()

	for b.Loop() {
		tracker.RecordLatency(time.Microsecond)
	}
}

/*
BenchmarkLatencyAverage measures rolling average computation over a full window.
*/
func BenchmarkLatencyAverage(b *testing.B) {
	tracker := NewLatencyTracker(100)

	for index := 0; index < 100; index++ {
		tracker.RecordLatency(time.Millisecond)
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = tracker.AverageLatency()
	}
}

package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMetricsRecordJobSuccess(t *testing.T) {
	Convey("Given fresh Metrics", t, func() {
		m := NewMetrics()

		Convey("RecordJobSuccess should track latency and success rate", func() {
			m.RecordJobSuccess(10 * time.Millisecond)
			m.RecordJobSuccess(20 * time.Millisecond)
			So(m.JobCount, ShouldEqual, 2)
			So(m.FailureCount, ShouldEqual, 0)
			So(m.JobSuccessRate, ShouldEqual, 1.0)
			So(m.AverageJobLatency, ShouldEqual, 15*time.Millisecond)
			So(m.P95JobLatency, ShouldBeGreaterThan, 0)
			So(m.TotalJobTime, ShouldEqual, 30*time.Millisecond)
		})
	})
}

func TestMetricsRecordJobFailureAfterSuccess(t *testing.T) {
	Convey("Given Metrics with a recorded success", t, func() {
		m := NewMetrics()
		m.RecordJobSuccess(time.Millisecond)
		m.RecordJobFailure()

		Convey("Failure should decrement success rate", func() {
			So(m.JobCount, ShouldEqual, 1)
			So(m.FailureCount, ShouldEqual, 1)
			So(m.JobSuccessRate, ShouldEqual, 0.0)
		})
	})
}

func TestMetricsRecordJobFailureWithoutSuccess(t *testing.T) {
	Convey("Given Metrics with only failures recorded", t, func() {
		m := NewMetrics()
		m.RecordJobFailure()

		Convey("Success rate should be zero when no successes recorded", func() {
			So(m.JobCount, ShouldEqual, 0)
			So(m.FailureCount, ShouldEqual, 1)
			So(m.JobSuccessRate, ShouldEqual, 0.0)
		})
	})
}

func TestMetricsRecordJobExecutionSuccess(t *testing.T) {
	Convey("Given Metrics", t, func() {
		m := NewMetrics()
		start := time.Now().Add(-25 * time.Millisecond)

		Convey("RecordJobExecution should accumulate duration on success", func() {
			m.RecordJobExecution(start, true)
			So(m.JobCount, ShouldEqual, 1)
			So(m.FailureCount, ShouldEqual, 0)
			So(m.TotalJobTime, ShouldBeGreaterThan, 0)
			So(m.JobSuccessRate, ShouldEqual, 1.0)
		})
	})
}

func TestMetricsRecordJobExecutionWithoutSuccessFlag(t *testing.T) {
	Convey("Given Metrics", t, func() {
		m := NewMetrics()
		start := time.Now().Add(-10 * time.Millisecond)

		Convey("RecordJobExecution with success false still increments job count", func() {
			m.RecordJobExecution(start, false)
			So(m.JobCount, ShouldEqual, 1)
			So(m.FailureCount, ShouldEqual, 0)
			So(m.JobSuccessRate, ShouldEqual, 1.0)
		})
	})
}

func TestMetricsExportMetrics(t *testing.T) {
	Convey("Given Metrics with populated fields", t, func() {
		m := NewMetricsForExportTest(
			3,
			1,
			7,
			0.75,
			12*time.Millisecond,
			20*time.Millisecond,
			30*time.Millisecond,
			0.42,
		)

		exported := m.ExportMetrics()

		Convey("Export should include expected keys and values", func() {
			So(exported["worker_count"], ShouldEqual, 3)
			So(exported["idle_workers"], ShouldEqual, 1)
			So(exported["queue_size"], ShouldEqual, 7)
			So(exported["success_rate"], ShouldEqual, 0.75)
			So(exported["avg_latency"], ShouldEqual, int64(12))
			So(exported["p95_latency"], ShouldEqual, int64(20))
			So(exported["p99_latency"], ShouldEqual, int64(30))
			So(exported["resource_utilization"], ShouldEqual, 0.42)
		})
	})
}

func TestMetricsLatencyDigestCompressionPath(t *testing.T) {
	Convey("Given Metrics", t, func() {
		m := NewMetrics()
		m.SetMaxCentroids(8)
		m.SetCompression(2)

		Convey("Many samples should exercise centroid insert and compression", func() {
			for i := range 50 {
				m.RecordJobSuccess(time.Duration(i+1) * time.Millisecond)
			}
			So(m.JobCount, ShouldEqual, 50)
			So(m.CentroidCount(), ShouldBeGreaterThan, 0)
			So(m.P99JobLatency, ShouldBeGreaterThan, 0)
		})
	})
}

func TestMetricsLatencyDigestCoalescesIdenticalSamples(t *testing.T) {
	Convey("Given Metrics receiving identical latency samples", t, func() {
		m := NewMetrics()
		m.SetMaxCentroids(128)
		m.SetCompression(100)

		Convey("RecordJobSuccess should keep a single centroid for repeated values", func() {
			for range 64 {
				m.RecordJobSuccess(7 * time.Millisecond)
			}

			So(m.JobCount, ShouldEqual, 64)
			So(m.TotalWeight(), ShouldEqual, 64)
			So(m.CentroidCount(), ShouldEqual, 1)
			So(m.P95JobLatency, ShouldEqual, 7*time.Millisecond)
			So(m.P99JobLatency, ShouldEqual, 7*time.Millisecond)
		})
	})
}

func BenchmarkMetricsRecordJobSuccess(b *testing.B) {
	m := NewMetrics()
	d := 7 * time.Millisecond
	b.ReportAllocs()
	for b.Loop() {
		m.RecordJobSuccess(d)
	}
}

func BenchmarkMetricsExportMetrics(b *testing.B) {
	m := NewMetrics()
	m.RecordJobSuccess(time.Millisecond)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.ExportMetrics()
	}
}

func BenchmarkMetricsRecordJobSuccessVaryingLatency(b *testing.B) {
	m := NewMetrics()
	var sample int64 = 1
	b.ReportAllocs()
	for b.Loop() {
		latency := time.Duration(sample) * time.Millisecond
		sample++
		if sample > 512 {
			sample = 1
		}
		m.RecordJobSuccess(latency)
	}
}

func BenchmarkMetricsRecordJobExecution(b *testing.B) {
	m := NewMetrics()
	b.ReportAllocs()
	for b.Loop() {
		start := time.Now().Add(-2 * time.Millisecond)
		m.RecordJobExecution(start, true)
	}
}

func BenchmarkMetricsRecordJobFailure(b *testing.B) {
	m := NewMetrics()
	m.RecordJobSuccess(time.Millisecond)
	b.ReportAllocs()
	for b.Loop() {
		m.RecordJobFailure()
	}
}

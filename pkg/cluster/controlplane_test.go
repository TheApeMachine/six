package cluster

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/theapemachine/six/pkg/core"
)

func TestNewControlPlane(t *testing.T) {
	Convey("NewControlPlane", t, func() {
		Convey("returns a non-nil control plane", func() {
			controlPlane := NewControlPlane(t.Context())
			So(controlPlane, ShouldNotBeNil)
		})
	})
}

func TestControlPlaneInsert(t *testing.T) {
	Convey("Insert", t, func() {
		Convey("accepts remote affinities after local bootstrap", func() {
			controlPlane := NewControlPlane(t.Context())
			local := clusterTestValue(0x1010, 1)
			remote := clusterTestValue(0x2020, 2)

			controlPlane.Insert(0x1010, local)
			controlPlane.Insert(0x2020, remote)

			found := controlPlane.FindClosest(0x2020)
			So(len(found), ShouldEqual, 1)
			So(found[0][core.Cfg.Value.Region.Affinity.Start], ShouldEqual, uint64(0x2020))
		})
	})
}

func TestControlPlaneFindClosest(t *testing.T) {
	Convey("FindClosest", t, func() {
		Convey("returns Values ordered by routing table proximity", func() {
			controlPlane := NewControlPlane(t.Context())
			controlPlane.Insert(0x3030, clusterTestValue(0x3030, 1))
			controlPlane.Insert(0x4040, clusterTestValue(0x4040, 2))
			controlPlane.Insert(0x5050, clusterTestValue(0x5050, 3))

			found := controlPlane.FindClosest(0x4000)
			So(len(found), ShouldBeGreaterThanOrEqualTo, 1)
			So(found[0][core.Cfg.Value.Region.Affinity.Start], ShouldEqual, uint64(0x4040))
		})
	})
}

func BenchmarkControlPlaneInsert(b *testing.B) {
	controlPlane := NewControlPlane(b.Context())
	controlPlane.Insert(0x6000, clusterTestValue(0x6000, 0))

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		controlPlane.Insert(uint64(iteration),
			clusterTestValue(0x7000+uint64(iteration%1024), uint64(iteration)),
		)
	}
}

func BenchmarkControlPlaneFindClosest(b *testing.B) {
	controlPlane := NewControlPlane(b.Context())

	for index := 0; index < 32; index++ {
		controlPlane.Insert(
			uint64(index),
			clusterTestValue(0x8000+uint64(index), uint64(index)),
		)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = controlPlane.FindClosest(0x8010 + uint64(iteration%8))
	}
}

package distributed

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewDiscovery(t *testing.T) {
	Convey("NewDiscovery", t, func() {
		Convey("seeds the node map with the local node", func() {
			discovery := NewDiscovery(
				t.Context(),
				DiscoveryWithNodeID("alpha"),
				DiscoveryWithAdvertiseAddr("10.0.0.12:9000"),
				DiscoveryWithCapacity(4),
			)
			So(discovery, ShouldNotBeNil)
			So(discovery.Self().ID, ShouldEqual, "alpha")
			So(discovery.Self().Addr, ShouldEqual, "10.0.0.12:9000")
			So(discovery.Self().Self, ShouldBeTrue)
		})

		Convey("enforces ttl above heartbeat", func() {
			discovery := NewDiscovery(
				t.Context(),
				DiscoveryWithHeartbeat(2*time.Second),
				DiscoveryWithTTL(1*time.Nanosecond),
			)
			So(discovery, ShouldNotBeNil)
			So(discovery.ttl, ShouldBeGreaterThanOrEqualTo, discovery.heartbeat)
		})
	})
}

func TestDiscoveryNodes(t *testing.T) {
	Convey("Nodes", t, func() {
		Convey("can omit the self node when requested", func() {
			discovery := NewDiscovery(t.Context())

			all := discovery.Nodes(true)
			remote := discovery.Nodes(false)

			So(len(all), ShouldBeGreaterThanOrEqualTo, 1)
			So(len(remote), ShouldEqual, 0)
		})
	})
}

func TestDiscoverySelf(t *testing.T) {
	Convey("Self", t, func() {
		Convey("returns capacity and shard fields from options", func() {
			discovery := NewDiscovery(
				t.Context(),
				DiscoveryWithNodeID("n1"),
				DiscoveryWithCapacity(8),
				DiscoveryWithAffinityShard(0x100, 2),
			)

			self := discovery.Self()
			So(self.Capacity, ShouldEqual, 8)
			So(self.ShardBits, ShouldEqual, uint8(2))
			So(self.ShardMask, ShouldEqual, uint64(0x100))
		})
	})
}

func TestDiscoveryCapacity(t *testing.T) {
	Convey("Capacity", t, func() {
		Convey("reflects the configured scheduler depth", func() {
			discovery := NewDiscovery(
				t.Context(),
				DiscoveryWithCapacity(11),
			)
			So(discovery.Capacity(), ShouldEqual, 11)
		})
	})
}

func TestDiscoverySetCapacity(t *testing.T) {
	Convey("SetCapacity", t, func() {
		Convey("clamps negative input and updates the self node", func() {
			discovery := NewDiscovery(
				t.Context(),
				DiscoveryWithCapacity(5),
			)
			So(discovery.SetCapacity(-3), ShouldEqual, 0)
			So(discovery.Self().Capacity, ShouldEqual, 0)
		})
	})
}

func BenchmarkDiscoverySelf(b *testing.B) {
	discovery := NewDiscovery(b.Context())

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = discovery.Self()
	}
}

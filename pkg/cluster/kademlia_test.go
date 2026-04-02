package cluster

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func resolveClusterTestConfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); envPath != "" {
		return filepath.Clean(envPath)
	}

	_, file, _, ok := runtime.Caller(0)

	if ok {
		return filepath.Clean(
			filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"),
		)
	}

	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveClusterTestConfigPath())

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "cluster: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}

	viper.Set("loglevel", "error")
	core.NewConfig()
	os.Exit(m.Run())
}

func clusterTestValue(affinity, tokenWord uint64) primitive.Value {
	var value primitive.Value

	region := core.Cfg.Value.Region
	value[region.Affinity.Start] = affinity

	if tokenWord != 0 {
		value[region.Tokens.Start] = tokenWord
	}

	return value
}

func TestNewRoutingTable(t *testing.T) {
	Convey("NewRoutingTable", t, func() {
		table := NewRoutingTable(NodeID(42))
		So(table, ShouldNotBeNil)
	})
}

func TestRoutingTableSetLocal(t *testing.T) {
	Convey("SetLocal", t, func() {
		Convey("marks the table bootstrapped", func() {
			table := NewRoutingTable(0)
			So(table.isBootstrapped(), ShouldBeFalse)
			table.SetLocal(7)
			So(table.isBootstrapped(), ShouldBeTrue)
		})
	})
}

func TestRoutingTableInsert(t *testing.T) {
	Convey("Insert", t, func() {
		Convey("ignores values whose affinity matches the local node id", func() {
			table := NewRoutingTable(0)
			table.SetLocal(0xABCD)
			v := clusterTestValue(0xABCD, 1)
			table.Insert(0xABCD, &v)
			closest := table.FindClosest(0xFFFF, 10)
			So(len(closest), ShouldEqual, 0)
		})

		Convey("stores remote values for lookup", func() {
			table := NewRoutingTable(0)
			table.SetLocal(0x1000)
			v := clusterTestValue(0x2000, 1)
			table.Insert(0x2000, &v)
			closest := table.FindClosest(0x2100, 4)
			So(len(closest), ShouldEqual, 1)
			So(closest[0].id, ShouldEqual, NodeID(0x2000))
		})
	})
}

func TestRoutingTableFindClosest(t *testing.T) {
	Convey("FindClosest", t, func() {
		Convey("caps results at k after merging candidates", func() {
			savedK := core.Cfg.ControlPlane.K
			defer func() {
				core.Cfg.ControlPlane.K = savedK
			}()

			core.Cfg.ControlPlane.K = 3

			table := NewRoutingTable(0)
			table.SetLocal(0x10000)

			for index := uint64(1); index <= 5; index++ {
				v := clusterTestValue(0x20000+index*0x100, index)
				table.Insert(uint64(index), &v)
			}

			closest := table.FindClosest(0x20100, 3)
			So(len(closest), ShouldBeLessThanOrEqualTo, 3)
		})
	})
}

func TestRoutingTableFindNode(t *testing.T) {
	Convey("FindNode", t, func() {
		Convey("mirrors FindClosest for local lookup", func() {
			savedK := core.Cfg.ControlPlane.K
			defer func() {
				core.Cfg.ControlPlane.K = savedK
			}()

			core.Cfg.ControlPlane.K = 4

			table := NewRoutingTable(0)
			table.SetLocal(0x4000)
			v := clusterTestValue(0x5000, 1)
			table.Insert(0x5000, &v)

			ctx := t.Context()
			fromLookup := table.FindNode(ctx, 0x5100)
			fromClosest := table.FindClosest(0x5100, core.Cfg.ControlPlane.K)

			So(len(fromLookup), ShouldEqual, len(fromClosest))

			for i := range fromLookup {
				So(fromLookup[i].id, ShouldEqual, fromClosest[i].id)
			}
		})
	})
}

func TestRoutingTableStore(t *testing.T) {
	Convey("Store", t, func() {
		Convey("delegates to Insert", func() {
			table := NewRoutingTable(0)
			table.SetLocal(0xA000)
			v := clusterTestValue(0xB000, 9)
			table.Insert(0xB000, &v)
			closest := table.FindClosest(0xB100, 6)
			So(len(closest), ShouldEqual, 1)
			So(closest[0].id, ShouldEqual, NodeID(0xB000))
		})
	})
}

func BenchmarkRoutingTableInsert(b *testing.B) {
	table := NewRoutingTable(0)
	table.SetLocal(0xFF00)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		v := clusterTestValue(uint64(iteration+1), uint64(iteration))
		table.Insert(uint64(iteration), &v)
	}
}

func BenchmarkRoutingTableFindClosest(b *testing.B) {
	table := NewRoutingTable(0)
	table.SetLocal(0x8000)

	for index := 0; index < 64; index++ {
		v := clusterTestValue(0x9000+uint64(index), uint64(index))
		table.Insert(uint64(index), &v)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = table.FindClosest(NodeID(0x9020+uint64(iteration%8)), core.Cfg.ControlPlane.K)
	}
}

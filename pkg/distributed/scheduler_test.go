package distributed

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestMain(m *testing.M) {
	viper.SetConfigFile("../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "distributed/scheduler_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestCandidateOrderUsesAffinityPrefix(t *testing.T) {
	nodes := []Node{
		{ID: "n0", Addr: "n0:1", Capacity: 1},
		{ID: "n1", Addr: "n1:1", Capacity: 1},
		{ID: "n2", Addr: "n2:1", Capacity: 1},
		{ID: "n3", Addr: "n3:1", Capacity: 1},
	}

	left := makeFrameWithAffinity(3, len(nodes))
	ordered := candidateOrder(nodes, left, nil, 0)
	if len(ordered) != len(nodes) {
		t.Fatalf("candidate count mismatch: got %d want %d", len(ordered), len(nodes))
	}
	if got := ordered[0].ID; got != "n3" {
		t.Fatalf("first candidate mismatch: got %s want n3", got)
	}
}

func TestCandidateOrderFallsBackToRoundRobinWithoutAffinity(t *testing.T) {
	nodes := []Node{
		{ID: "n0", Addr: "n0:1", Capacity: 1},
		{ID: "n1", Addr: "n1:1", Capacity: 1},
		{ID: "n2", Addr: "n2:1", Capacity: 1},
	}

	left := make([]byte, primitive.ByteSize)
	ordered := candidateOrder(nodes, left, nil, 1)
	if got := ordered[0].ID; got != "n1" {
		t.Fatalf("round-robin fallback mismatch: got %s want n1", got)
	}
}

func TestCandidateOrderKeepsUniqueNodesUnderCapacityWeighting(t *testing.T) {
	nodes := []Node{
		{ID: "n0", Addr: "n0:1", Capacity: 2},
		{ID: "n1", Addr: "n1:1", Capacity: 1},
		{ID: "n2", Addr: "n2:1", Capacity: 1},
	}

	left := makeFrameWithAffinity(0, 4)
	ordered := candidateOrder(nodes, left, nil, 0)
	if len(ordered) != len(nodes) {
		t.Fatalf("unique weighted candidate count mismatch: got %d want %d", len(ordered), len(nodes))
	}
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range ordered {
		seen[node.ID] = struct{}{}
	}
	if len(seen) != len(nodes) {
		t.Fatalf("expected each node exactly once, got %d unique nodes", len(seen))
	}
	if got := ordered[0].ID; got != "n0" {
		t.Fatalf("weighted first candidate mismatch: got %s want n0", got)
	}
}

func TestCandidateOrderPrefersMatchingShardOwners(t *testing.T) {
	nodes := []Node{
		{ID: "fallback", Addr: "fallback:1", Capacity: 3},
		{ID: "owner-a", Addr: "owner-a:1", Capacity: 1, ShardBits: 2, ShardMask: affinityForRegion(1, 4)},
		{ID: "owner-b", Addr: "owner-b:1", Capacity: 1, ShardBits: 2, ShardMask: affinityForRegion(3, 4)},
	}

	left := makeFrameWithAffinity(3, 4)
	ordered := candidateOrder(nodes, left, nil, 0)
	if len(ordered) != 1 {
		t.Fatalf("matching shard candidate count mismatch: got %d want 1", len(ordered))
	}
	if got := ordered[0].ID; got != "owner-b" {
		t.Fatalf("matching shard owner mismatch: got %s want owner-b", got)
	}
}

func TestCandidateOrderFallsBackWhenNoShardMatches(t *testing.T) {
	nodes := []Node{
		{ID: "n0", Addr: "n0:1", Capacity: 1, ShardBits: 2, ShardMask: affinityForRegion(0, 4)},
		{ID: "n1", Addr: "n1:1", Capacity: 1, ShardBits: 2, ShardMask: affinityForRegion(1, 4)},
		{ID: "n2", Addr: "n2:1", Capacity: 1},
	}

	left := makeFrameWithAffinity(3, 4)
	ordered := candidateOrder(nodes, left, nil, 0)
	if len(ordered) != len(nodes) {
		t.Fatalf("fallback candidate count mismatch: got %d want %d", len(ordered), len(nodes))
	}
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range ordered {
		seen[node.ID] = struct{}{}
	}
	if len(seen) != len(nodes) {
		t.Fatalf("fallback should retain all nodes, got %d unique nodes", len(seen))
	}
}

func makeFrameWithAffinity(region, regions int) []byte {
	frame := make([]byte, primitive.ByteSize)
	affinity := affinityForRegion(region, regions)
	binary.LittleEndian.PutUint64(frame[core.Cfg.Value.Region.Affinity.Start*8:], affinity)
	return frame
}

func affinityForRegion(region, regions int) uint64 {
	var affinity uint64
	if regions > 1 {
		routeBits := 0
		for (1 << routeBits) < regions {
			routeBits++
		}
		shift := 48 - routeBits
		if shift < 0 {
			shift = 0
		}
		affinity = uint64(region) << shift
	}
	return affinity
}

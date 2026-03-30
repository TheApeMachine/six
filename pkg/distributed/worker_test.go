package distributed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleHealthIncludesShardOwnership(t *testing.T) {
	discovery := NewDiscovery(
		t.Context(),
		DiscoveryWithNodeID("node-1"),
		DiscoveryWithAdvertiseAddr("127.0.0.1:7777"),
		DiscoveryWithCapacity(3),
		DiscoveryWithAffinityShard(0xC00000000000, 2),
	)

	worker, err := NewWorker(
		t.Context(),
		WorkerWithListenAddr("127.0.0.1:7777"),
		WorkerWithAdvertiseAddr("127.0.0.1:7777"),
		WorkerWithDiscovery(discovery),
	)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rr := httptest.NewRecorder()

	worker.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status mismatch: got %d want %d", rr.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got := payload["node_id"]; got != "node-1" {
		t.Fatalf("node_id mismatch: got %v want node-1", got)
	}
	if got := int(payload["capacity"].(float64)); got != 3 {
		t.Fatalf("capacity mismatch: got %d want 3", got)
	}
	if got := int(payload["shard_bits"].(float64)); got != 2 {
		t.Fatalf("shard_bits mismatch: got %d want 2", got)
	}
	if got := uint64(payload["shard_mask"].(float64)); got != 0xC00000000000 {
		t.Fatalf("shard_mask mismatch: got %#x want %#x", got, uint64(0xC00000000000))
	}
	if got := payload["shard_label"]; got != "11/2" {
		t.Fatalf("shard_label mismatch: got %v want 11/2", got)
	}
}

func TestHandleNodesIncludesShardOwnership(t *testing.T) {
	discovery := NewDiscovery(
		t.Context(),
		DiscoveryWithNodeID("node-1"),
		DiscoveryWithAdvertiseAddr("127.0.0.1:7777"),
		DiscoveryWithCapacity(3),
		DiscoveryWithAffinityShard(0x800000000000, 2),
	)

	worker, err := NewWorker(
		t.Context(),
		WorkerWithListenAddr("127.0.0.1:7777"),
		WorkerWithAdvertiseAddr("127.0.0.1:7777"),
		WorkerWithDiscovery(discovery),
	)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/nodes", nil)
	rr := httptest.NewRecorder()

	worker.handleNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status mismatch: got %d want %d", rr.Code, http.StatusOK)
	}

	var nodes []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("node count mismatch: got %d want 1", len(nodes))
	}
	if got := int(nodes[0]["shard_bits"].(float64)); got != 2 {
		t.Fatalf("shard_bits mismatch: got %d want 2", got)
	}
	if got := uint64(nodes[0]["shard_mask"].(float64)); got != 0x800000000000 {
		t.Fatalf("shard_mask mismatch: got %#x want %#x", got, uint64(0x800000000000))
	}
	if got := nodes[0]["shard_label"]; got != "10/2" {
		t.Fatalf("shard_label mismatch: got %v want 10/2", got)
	}
}

func TestNewWorkerAutoAssignsShardFromAdvertiseAddr(t *testing.T) {
	worker, err := NewWorker(
		t.Context(),
		WorkerWithListenAddr("127.0.0.1:7777"),
		WorkerWithAdvertiseAddr("127.0.0.1:7777"),
		WorkerWithAutoAffinityShard(3),
	)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	wantMask := autoShardMask(3, "127.0.0.1:7777")
	if got := worker.shardBits; got != 3 {
		t.Fatalf("shard_bits mismatch: got %d want 3", got)
	}
	if got := worker.shardMask; got != wantMask {
		t.Fatalf("shard_mask mismatch: got %#x want %#x", got, wantMask)
	}
	if got := shardLabel(worker.shardMask, worker.shardBits); got != shardLabel(wantMask, 3) {
		t.Fatalf("shard_label mismatch: got %q want %q", got, shardLabel(wantMask, 3))
	}
	if got := worker.discovery.Self().ShardMask; got != wantMask {
		t.Fatalf("discovery shard_mask mismatch: got %#x want %#x", got, wantMask)
	}
}

func TestNewWorkerPrefersManualShardOverAuto(t *testing.T) {
	worker, err := NewWorker(
		t.Context(),
		WorkerWithListenAddr("127.0.0.1:7777"),
		WorkerWithAdvertiseAddr("127.0.0.1:7777"),
		WorkerWithAutoAffinityShard(3),
		WorkerWithAffinityShard(0xA00000000000, 3),
	)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	if got := worker.shardBits; got != 3 {
		t.Fatalf("shard_bits mismatch: got %d want 3", got)
	}
	if got := worker.shardMask; got != 0xA00000000000 {
		t.Fatalf("manual shard_mask mismatch: got %#x want %#x", got, uint64(0xA00000000000))
	}
}

func TestShardLabel(t *testing.T) {
	tests := []struct {
		name string
		mask uint64
		bits uint8
		want string
	}{
		{name: "unassigned", mask: 0, bits: 0, want: "unassigned"},
		{name: "two bit prefix", mask: 0xC00000000000, bits: 2, want: "11/2"},
		{name: "three bit prefix", mask: 0xA00000000000, bits: 3, want: "101/3"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shardLabel(tc.mask, tc.bits); got != tc.want {
				t.Fatalf("shardLabel mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

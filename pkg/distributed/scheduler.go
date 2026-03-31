package distributed

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

type schedulerOption func(*Scheduler)

type Scheduler struct {
	discovery *Discovery
	client    *http.Client
	path      string
	rr        atomic.Uint64
}

func NewScheduler(discovery *Discovery, opts ...schedulerOption) *Scheduler {
	s := &Scheduler{
		discovery: discovery,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		path: DefaultServicePath,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.client == nil {
		s.client = &http.Client{Timeout: 5 * time.Second}
	}
	if strings.TrimSpace(s.path) == "" {
		s.path = DefaultServicePath
	}
	return s
}

func SchedulerWithHTTPClient(client *http.Client) schedulerOption {
	return func(s *Scheduler) {
		s.client = client
	}
}

func SchedulerWithPath(path string) schedulerOption {
	return func(s *Scheduler) {
		s.path = path
	}
}

func (s *Scheduler) Nodes() []Node {
	if s == nil || s.discovery == nil {
		return nil
	}
	return s.discovery.Nodes(false)
}

func (s *Scheduler) ScheduleUniversalBitwise(
	ctx context.Context, left, right []byte,
) (*UniversalBitwiseJobResponse, error) {
	if len(left) != primitive.ByteSize {
		return nil, fmt.Errorf("left frame size %d, want %d", len(left), primitive.ByteSize)
	}
	if len(right) > 0 && len(right) != primitive.ByteSize {
		return nil, fmt.Errorf("right frame size %d, want %d", len(right), primitive.ByteSize)
	}

	nodes := s.Nodes()
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no remote mesh nodes discovered")
	}

	candidates := candidateOrder(nodes, left, right, s.rr.Add(1)-1)

	telemetry.Emit(telemetry.Event{
		Component: "Pool",
		Action:    "Dispatch",
		Data: telemetry.EventData{
			TaskType:  "UniversalBitwise",
			NodeCount: len(nodes),
		},
	})

	var lastErr error

	// Basic concurrent scheduling hedging to avoid head-of-line blocking
	// from bad nodes hanging HTTP connections.
	type result struct {
		resp *UniversalBitwiseJobResponse
		err  error
	}
	resCh := make(chan result, len(candidates))
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, n := range candidates {
		go func(node Node) {
			resp, err := s.tryNode(childCtx, node, left, right)
			resCh <- result{resp, err}
		}(n)
	}

	for i := 0; i < len(candidates); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-resCh:
			if r.err == nil {
				return r.resp, nil
			}
			lastErr = r.err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("all remote nodes rejected job")
	}
	return nil, lastErr
}

func (s *Scheduler) tryNode(
	ctx context.Context,
	node Node,
	left, right []byte,
) (*UniversalBitwiseJobResponse, error) {
	reqBody := make([]byte, primitive.ByteSize*2)
	copy(reqBody[:primitive.ByteSize], left)
	if len(right) > 0 {
		copy(reqBody[primitive.ByteSize:], right)
	}

	url := toHTTPURL(node.Addr, s.path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	httpResp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 2<<10))
		return nil, fmt.Errorf("node %s status %d: %s", node.Addr, httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	respBody := make([]byte, primitive.ByteSize*2)
	n, err := io.ReadFull(httpResp.Body, respBody)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("node %s read response: %w", node.Addr, err)
	}
	if n != primitive.ByteSize*2 {
		return nil, fmt.Errorf("node %s returned invalid frame sizes left+right=%d", node.Addr, n)
	}

	durationMs := int64(0)
	if raw := strings.TrimSpace(httpResp.Header.Get("X-Six-Duration-Ms")); raw != "" {
		if ms, parseErr := time.ParseDuration(raw + "ms"); parseErr == nil {
			durationMs = int64(ms / time.Millisecond)
		}
	}
	nodeID := strings.TrimSpace(httpResp.Header.Get("X-Six-Node-ID"))
	if nodeID == "" {
		nodeID = node.ID
	}

	return &UniversalBitwiseJobResponse{
		NodeID:     nodeID,
		DurationMS: durationMs,
		Left:       respBody[:primitive.ByteSize],
		Right:      respBody[primitive.ByteSize:],
	}, nil
}

func toHTTPURL(addr, path string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/") + path
	}
	return "http://" + addr + path
}

func candidateOrder(nodes []Node, left, right []byte, rr uint64) []Node {
	routingAffinity := frameRoutingAffinity(left, right)
	nodes = preferAffinityShard(nodes, routingAffinity)
	if len(nodes) == 0 {
		return nil
	}

	positive := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Capacity > 0 {
			positive = append(positive, n)
		}
	}
	if len(positive) == 0 {
		return rotateNodes(nodes, affinityStartIndex(routingAffinity, len(nodes), rr))
	}

	// When nodes do not advertise explicit shard ownership, the routing rules
	// are prefix-directed round-robin rather than ShardMask XOR sorting. This
	// keeps stream/scheduler semantics aligned with the region-prefix routing
	// described in IDEAS.md and the existing scheduler tests.
	hasExplicitShards := false
	for _, n := range positive {
		if n.ShardBits > 0 {
			hasExplicitShards = true
			break
		}
	}
	if !hasExplicitShards {
		return rotateNodes(positive, affinityStartIndex(routingAffinity, len(positive), rr))
	}

	// Kademlia XOR-distance sort: prefer nodes whose shard mask is closest
	// to the routing affinity, measured by XOR distance (lower = closer).
	sortByXORDistance(positive, routingAffinity)

	return positive
}

func preferAffinityShard(nodes []Node, affinity uint64) []Node {
	if len(nodes) == 0 {
		return nil
	}
	matched := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if nodeOwnsAffinity(node, affinity) {
			matched = append(matched, node)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return nodes
}

func rotateNodes(nodes []Node, start int) []Node {
	if len(nodes) <= 1 {
		return nodes
	}
	if start < 0 {
		start = 0
	}
	start %= len(nodes)
	out := make([]Node, 0, len(nodes))
	out = append(out, nodes[start:]...)
	out = append(out, nodes[:start]...)
	return out
}

func frameRoutingAffinity(left, right []byte) uint64 {
	affinity := frameAffinity(left)
	if affinity != 0 {
		return affinity
	}
	return frameAffinity(right)
}

func frameAffinity(frame []byte) uint64 {
	start := core.Cfg.AffinityIndex * 8
	end := start + 8
	if start < 0 || end > len(frame) {
		return 0
	}
	return binary.LittleEndian.Uint64(frame[start:end]) & 0x0000FFFFFFFFFFFF
}

func affinityStartIndex(affinity uint64, count int, fallback uint64) int {
	if count <= 1 {
		return 0
	}
	if affinity == 0 {
		return int(fallback % uint64(count))
	}
	routeBits := bits.Len(uint(count - 1))
	if routeBits <= 0 {
		return 0
	}
	shift := 48 - routeBits
	if shift < 0 {
		shift = 0
	}
	idx := int((affinity >> shift) & ((1 << routeBits) - 1))
	if idx >= count {
		idx %= count
	}
	return idx
}

func nodeOwnsAffinity(node Node, affinity uint64) bool {
	if node.ShardBits == 0 {
		return false
	}
	if node.ShardBits > 48 {
		return false
	}
	mask := prefixMask(node.ShardBits)
	if mask == 0 {
		return false
	}
	return (affinity & mask) == (node.ShardMask & mask)
}

func prefixMask(b uint8) uint64 {
	if b == 0 {
		return 0
	}
	if b >= 48 {
		return 0x0000FFFFFFFFFFFF
	}
	return ((uint64(1) << b) - 1) << (48 - b)
}

// sortByXORDistance sorts nodes so that those whose ShardMask is closest
// (by XOR distance) to the target affinity come first. This implements
// Kademlia-style O(log N) directed routing from IDEAS.md §1.
func sortByXORDistance(nodes []Node, targetAffinity uint64) {
	target := targetAffinity & 0x0000FFFFFFFFFFFF
	sort.Slice(nodes, func(i, j int) bool {
		di := (nodes[i].ShardMask & 0x0000FFFFFFFFFFFF) ^ target
		dj := (nodes[j].ShardMask & 0x0000FFFFFFFFFFFF) ^ target
		return di < dj
	})
}

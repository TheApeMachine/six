package distributed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

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

	candidates := candidateOrder(nodes, s.rr.Add(1)-1)

	telemetry.Emit(telemetry.Event{
		Component: "Pool",
		Action:    "Dispatch",
		Data: telemetry.EventData{
			TaskType:  "UniversalBitwise",
			NodeCount: len(nodes),
		},
	})

	var lastErr error

	for _, n := range candidates {
		resp, err := s.tryNode(ctx, n, left, right)
		if err == nil {
			return resp, nil
		}
		lastErr = err
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
	reqBody, err := json.Marshal(UniversalBitwiseJobRequest{
		Left:  left,
		Right: right,
	})
	if err != nil {
		return nil, err
	}

	url := toHTTPURL(node.Addr, s.path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 2<<20))
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node %s status %d: %s", node.Addr, httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var resp UniversalBitwiseJobResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("node %s: %s", node.Addr, resp.Error)
	}
	if len(resp.Left) != primitive.ByteSize || len(resp.Right) != primitive.ByteSize {
		return nil, fmt.Errorf("node %s returned invalid frame sizes left=%d right=%d", node.Addr, len(resp.Left), len(resp.Right))
	}
	return &resp, nil
}

func toHTTPURL(addr, path string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/") + path
	}
	return "http://" + addr + path
}

func candidateOrder(nodes []Node, rr uint64) []Node {
	positive := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Capacity > 0 {
			positive = append(positive, n)
		}
	}
	if len(positive) == 0 {
		return rotateNodes(nodes, rr)
	}

	weighted := make([]Node, 0, len(positive)*2)
	for _, n := range positive {
		weight := n.Capacity
		if weight > 8 {
			weight = 8
		}
		for i := 0; i < weight; i++ {
			weighted = append(weighted, n)
		}
	}

	start := int(rr % uint64(len(weighted)))
	seen := make(map[string]struct{}, len(positive))
	out := make([]Node, 0, len(positive))
	for i := 0; i < len(weighted) && len(out) < len(positive); i++ {
		n := weighted[(start+i)%len(weighted)]
		if _, ok := seen[n.ID]; ok {
			continue
		}
		seen[n.ID] = struct{}{}
		out = append(out, n)
	}
	return out
}

func rotateNodes(nodes []Node, rr uint64) []Node {
	if len(nodes) <= 1 {
		return nodes
	}
	start := int(rr % uint64(len(nodes)))
	out := make([]Node, 0, len(nodes))
	out = append(out, nodes[start:]...)
	out = append(out, nodes[:start]...)
	return out
}

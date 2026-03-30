package distributed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

type workerOption func(*Worker)

type Worker struct {
	ctx           context.Context
	cancel        context.CancelFunc
	listenAddr    string
	advertiseAddr string
	maxCapacity   int
	shardBits     uint8
	shardMask     uint64
	autoShardBits uint8
	inFlight      atomic.Int64
	discovery     *Discovery
	server        *http.Server
	path          string
}

func NewWorker(ctx context.Context, opts ...workerOption) (*Worker, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	w := &Worker{
		ctx:        ctx,
		cancel:     cancel,
		listenAddr: ":7777",
		path:       DefaultServicePath,
	}

	for _, opt := range opts {
		opt(w)
	}

	if strings.TrimSpace(w.listenAddr) == "" {
		return nil, fmt.Errorf("worker listen address is required")
	}

	if strings.TrimSpace(w.advertiseAddr) == "" {
		addr, err := ResolveAdvertiseAddr(w.listenAddr)
		if err != nil {
			return nil, fmt.Errorf("resolve advertise address: %w", err)
		}
		w.advertiseAddr = addr
	}

	if w.maxCapacity <= 0 && w.discovery != nil {
		w.maxCapacity = w.discovery.Self().Capacity
	}
	if w.discovery != nil && w.shardBits == 0 && w.shardMask == 0 {
		self := w.discovery.Self()
		w.shardBits = self.ShardBits
		w.shardMask = self.ShardMask & 0x0000FFFFFFFFFFFF
	}
	if w.shardBits == 0 && w.shardMask == 0 && w.autoShardBits > 0 {
		w.shardBits = min(w.autoShardBits, 48)
		w.shardMask = autoShardMask(w.shardBits, autoShardIdentity(w.advertiseAddr, w.discovery))
	}
	if w.maxCapacity <= 0 {
		w.maxCapacity = max(1, runtime.NumCPU()-1)
	}

	if w.discovery == nil {
		w.discovery = NewDiscovery(
			w.ctx,
			DiscoveryWithAdvertiseAddr(w.advertiseAddr),
			DiscoveryWithCapacity(w.maxCapacity),
			DiscoveryWithAffinityShard(w.shardMask, w.shardBits),
		)
	} else {
		w.discovery.shardBits = w.shardBits
		w.discovery.shardMask = w.shardMask & 0x0000FFFFFFFFFFFF
		w.discovery.SetCapacity(w.maxCapacity)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", w.handleHealth)
	mux.HandleFunc("/v1/nodes", w.handleNodes)
	mux.HandleFunc(w.path, w.handleUniversalBitwise)

	w.server = &http.Server{
		Addr:              w.listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}

	return w, nil
}

func WorkerWithListenAddr(addr string) workerOption {
	return func(w *Worker) {
		w.listenAddr = strings.TrimSpace(addr)
	}
}

func WorkerWithAdvertiseAddr(addr string) workerOption {
	return func(w *Worker) {
		w.advertiseAddr = strings.TrimSpace(addr)
	}
}

func WorkerWithCapacity(capacity int) workerOption {
	return func(w *Worker) {
		if capacity < 0 {
			capacity = 0
		}
		w.maxCapacity = capacity
	}
}

func WorkerWithDiscovery(discovery *Discovery) workerOption {
	return func(w *Worker) {
		w.discovery = discovery
	}
}

func WorkerWithAffinityShard(mask uint64, bits uint8) workerOption {
	return func(w *Worker) {
		if bits > 48 {
			bits = 48
		}
		w.shardBits = bits
		w.shardMask = mask & 0x0000FFFFFFFFFFFF
	}
}

func WorkerWithAutoAffinityShard(bits uint8) workerOption {
	return func(w *Worker) {
		if bits > 48 {
			bits = 48
		}
		w.autoShardBits = bits
	}
}

func WorkerWithPath(path string) workerOption {
	return func(w *Worker) {
		if p := strings.TrimSpace(path); p != "" {
			w.path = p
		}
	}
}

func (w *Worker) ListenAndServe() error {
	if err := w.discovery.Start(); err != nil {
		return err
	}

	go func() {
		<-w.ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = w.server.Shutdown(shutdownCtx)
	}()

	errnie.Info(
		"distributed.worker.start",
		"listen", w.listenAddr,
		"advertise", w.advertiseAddr,
		"group", w.discovery.discoveryGrp,
		"shard_bits", w.discovery.Self().ShardBits,
		"shard_mask", w.discovery.Self().ShardMask,
		"shard_label", shardLabel(w.discovery.Self().ShardMask, w.discovery.Self().ShardBits),
	)

	if err := w.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (w *Worker) Close() error {
	if w.cancel != nil {
		w.cancel()
	}
	var errs []error
	if w.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := w.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, err)
		}
	}
	if w.discovery != nil {
		if err := w.discovery.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errorsJoin(errs)
}

func (w *Worker) refreshAdvertisedCapacity(inFlight int64) {
	if w.discovery == nil {
		return
	}
	capacity := w.maxCapacity - int(inFlight)
	if capacity < 0 {
		capacity = 0
	}
	w.discovery.SetCapacity(capacity)
}

func (w *Worker) beginJob() func() {
	inFlight := w.inFlight.Add(1)
	w.refreshAdvertisedCapacity(inFlight)
	return func() {
		inFlight := w.inFlight.Add(-1)
		if inFlight < 0 {
			for {
				cur := w.inFlight.Load()
				if cur >= 0 {
					inFlight = cur
					break
				}
				if w.inFlight.CompareAndSwap(cur, 0) {
					inFlight = 0
					break
				}
			}
		}
		w.refreshAdvertisedCapacity(inFlight)
	}
}

func (w *Worker) handleHealth(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	self := w.discovery.Self()
	writeJSON(rw, http.StatusOK, map[string]any{
		"node_id":     self.ID,
		"addr":        self.Addr,
		"capacity":    self.Capacity,
		"shard_bits":  self.ShardBits,
		"shard_mask":  self.ShardMask,
		"shard_label": shardLabel(self.ShardMask, self.ShardBits),
		"timestamp":   time.Now().UnixNano(),
	})
}

func (w *Worker) handleNodes(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nodes := w.discovery.Nodes(true)
	payload := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		payload = append(payload, map[string]any{
			"id":          node.ID,
			"addr":        node.Addr,
			"capacity":    node.Capacity,
			"shard_bits":  node.ShardBits,
			"shard_mask":  node.ShardMask,
			"shard_label": shardLabel(node.ShardMask, node.ShardBits),
			"last_seen":   node.LastSeen,
			"self":        node.Self,
		})
	}
	writeJSON(rw, http.StatusOK, payload)
}

func shardLabel(mask uint64, bits uint8) string {
	if bits == 0 {
		return "unassigned"
	}
	if bits > 48 {
		bits = 48
	}
	prefix := mask >> (48 - bits)
	return fmt.Sprintf("%0*b/%d", bits, prefix, bits)
}

func autoShardIdentity(advertiseAddr string, discovery *Discovery) string {
	if discovery != nil {
		self := discovery.Self()
		if self.ID != "" {
			if advertiseAddr != "" {
				return self.ID + "@" + advertiseAddr
			}
			return self.ID
		}
	}
	return advertiseAddr
}

func autoShardMask(bits uint8, identity string) uint64 {
	if bits == 0 || identity == "" {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(identity))
	prefixMask := uint64(1<<bits) - 1
	prefix := h.Sum64() & prefixMask
	return prefix << (48 - bits)
}

func (w *Worker) handleUniversalBitwise(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	done := w.beginJob()
	defer done()

	body := make([]byte, primitive.ByteSize*2)
	n, err := io.ReadFull(req.Body, body)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		http.Error(rw, fmt.Sprintf("read request: %v", err), http.StatusBadRequest)
		return
	}

	if n != primitive.ByteSize && n != primitive.ByteSize*2 {
		http.Error(rw, fmt.Sprintf("invalid payload size %d, want %d or %d", n, primitive.ByteSize, primitive.ByteSize*2), http.StatusBadRequest)
		return
	}

	leftBytes := body[:primitive.ByteSize]
	var rightBytes []byte
	if n == primitive.ByteSize*2 {
		rightBytes = body[primitive.ByteSize:]
	} else {
		rightBytes = make([]byte, primitive.ByteSize)
	}

	start := time.Now()
	left := primitive.BytesToValue(leftBytes)
	right := primitive.BytesToValue(rightBytes)

	defer left.Release()
	defer right.Release()

	if err := compute.UniversalBitwise(unsafe.Pointer(left), unsafe.Pointer(right)); err != nil {
		dur := time.Since(start)
		telemetry.Emit(telemetry.Event{
			Component: "Pool",
			Action:    "JobFail",
			Data: telemetry.EventData{
				TaskType:   "UniversalBitwise",
				DurationMs: int(dur.Milliseconds()),
				NodeAddr:   w.advertiseAddr,
				Message:    err.Error(),
			},
		})
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
		return
	}

	dur := time.Since(start)
	telemetry.Emit(telemetry.Event{
		Component: "Pool",
		Action:    "JobDone",
		Data: telemetry.EventData{
			TaskType:   "UniversalBitwise",
			DurationMs: int(dur.Milliseconds()),
			NodeAddr:   w.advertiseAddr,
		},
	})

	rw.Header().Set("Content-Type", "application/octet-stream")
	rw.Header().Set("X-Six-Node-ID", w.discovery.Self().ID)
	rw.Header().Set("X-Six-Duration-Ms", fmt.Sprintf("%d", dur.Milliseconds()))
	rw.WriteHeader(http.StatusOK)

	leftOut := make([]byte, primitive.ByteSize)
	rightOut := make([]byte, primitive.ByteSize)
	_ = primitive.ValueToBytes(left, leftOut)
	_ = primitive.ValueToBytes(right, rightOut)

	rw.Write(leftOut)
	rw.Write(rightOut)
}

func writeJSON(rw http.ResponseWriter, status int, payload any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(payload)
}

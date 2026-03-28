package distributed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type workerOption func(*Worker)

type Worker struct {
	ctx           context.Context
	cancel        context.CancelFunc
	listenAddr    string
	advertiseAddr string
	discovery     *Discovery
	server        *http.Server
	path          string
}

func NewWorker(opts ...workerOption) (*Worker, error) {
	ctx, cancel := context.WithCancel(context.Background())
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

	if w.discovery == nil {
		w.discovery = NewDiscovery(
			DiscoveryWithContext(w.ctx),
			DiscoveryWithAdvertiseAddr(w.advertiseAddr),
			DiscoveryWithCapacity(max(1, runtime.NumCPU()-1)),
		)
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

func WorkerWithContext(ctx context.Context) workerOption {
	return func(w *Worker) {
		if w.cancel != nil {
			w.cancel()
		}
		w.ctx, w.cancel = context.WithCancel(ctx)
	}
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

func WorkerWithDiscovery(discovery *Discovery) workerOption {
	return func(w *Worker) {
		w.discovery = discovery
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
	if w.discovery != nil {
		if err := w.discovery.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if w.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := w.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, err)
		}
	}
	return errorsJoin(errs)
}

func (w *Worker) handleHealth(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	self := w.discovery.Self()
	writeJSON(rw, http.StatusOK, map[string]any{
		"node_id":   self.ID,
		"addr":      self.Addr,
		"capacity":  self.Capacity,
		"timestamp": time.Now().UnixNano(),
	})
}

func (w *Worker) handleNodes(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(rw, http.StatusOK, w.discovery.Nodes(true))
}

func (w *Worker) handleUniversalBitwise(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var job UniversalBitwiseJobRequest
	if err := json.NewDecoder(http.MaxBytesReader(rw, req.Body, 2<<20)).Decode(&job); err != nil {
		http.Error(rw, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return
	}

	if len(job.Left) != primitive.ByteSize {
		http.Error(rw, fmt.Sprintf("left frame size %d, want %d", len(job.Left), primitive.ByteSize), http.StatusBadRequest)
		return
	}
	if len(job.Right) == 0 {
		job.Right = make([]byte, primitive.ByteSize)
	}
	if len(job.Right) != primitive.ByteSize {
		http.Error(rw, fmt.Sprintf("right frame size %d, want %d", len(job.Right), primitive.ByteSize), http.StatusBadRequest)
		return
	}

	start := time.Now()
	left := primitive.BytesToValue(job.Left)
	right := primitive.BytesToValue(job.Right)

	if err := compute.UniversalBitwise(unsafe.Pointer(left), unsafe.Pointer(right)); err != nil {
		writeJSON(rw, http.StatusOK, UniversalBitwiseJobResponse{
			NodeID:     w.discovery.Self().ID,
			DurationMS: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return
	}

	leftOut := make([]byte, primitive.ByteSize)
	rightOut := make([]byte, primitive.ByteSize)
	_ = primitive.ValueToBytes(left, leftOut)
	_ = primitive.ValueToBytes(right, rightOut)

	writeJSON(rw, http.StatusOK, UniversalBitwiseJobResponse{
		NodeID:     w.discovery.Self().ID,
		DurationMS: time.Since(start).Milliseconds(),
		Left:       leftOut,
		Right:      rightOut,
	})
}

func writeJSON(rw http.ResponseWriter, status int, payload any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(payload)
}

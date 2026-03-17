package kernel

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/console"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
)

const (
	messageTypeWorkRequest  = 11
	messageTypeWorkResponse = 12

	messageErrNone      = 0
	messageErrInvalid   = 1
	messageErrCompute   = 2
	messageDataSize     = 32
	messagePointerCount = 5
)

// GFRotation size in bytes (A=uint16, B=uint16)
const nodeBytes = 4

type DistributedBackend struct {
	ctx    context.Context
	cancel context.CancelFunc
	pool   *pool.Pool
}

type distributedOpts func(*DistributedBackend)

func NewDistributedBackend(opts ...distributedOpts) (*DistributedBackend, error) {
	backend := &DistributedBackend{}

	for _, opt := range opts {
		opt(backend)
	}

	if backend.pool == nil {
		return nil, fmt.Errorf("DistributedBackend: DistributedWithPool must be provided")
	}

	return backend, nil
}

func (backend *DistributedBackend) Available() bool {
	return backend != nil && backend.pool != nil && len(config.System.Workers) > 0
}

func (backend *DistributedBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	contextPtr unsafe.Pointer,
) (uint64, error) {
	if backend == nil || backend.pool == nil {
		return 0, fmt.Errorf("distributed backend unavailable")
	}
	if numNodes <= 0 {
		return 0, fmt.Errorf("invalid numNodes: must be > 0")
	}
	if graphNodes == nil || contextPtr == nil {
		return 0, fmt.Errorf("invalid distributed pointers")
	}

	resolveCtx := backend.ctx
	if resolveCtx == nil {
		resolveCtx = context.Background()
	}

	rctx, cancel := context.WithTimeout(resolveCtx, time.Duration(config.System.Timeout)*time.Millisecond)
	defer cancel()

	ctxSlice := unsafe.Slice((*byte)(contextPtr), nodeBytes)
	ctxCopy := append([]byte(nil), ctxSlice...)

	chunkSize := max(config.System.Chunk, config.Numeric.VocabSize)
	timeout := time.Duration(config.System.Timeout) * time.Millisecond
	remoteOnly := config.System.RemoteOnly

	var best atomic.Uint64
	var localBuilder *Builder

	if !remoteOnly {
		backendForBuilder := config.System.Backend
		if backendForBuilder == "distributed" || backendForBuilder == "" {
			backendForBuilder = "cpu"
		}

		var localBak Backend
		switch backendForBuilder {
		case "metal":
			localBak = &metal.MetalBackend{}
		case "cuda":
			localBak = &cuda.CUDABackend{}
		case "cpu", "":
			localBak = &cpu.CPUBackend{}
		default:
			localBak = &cpu.CPUBackend{}
		}
		if !localBak.Available() {
			localBak = &cpu.CPUBackend{}
		}
		localBuilder = NewBuilder(WithBackend(localBak))
	}

	type chunkWork struct {
		start int
		end   int
	}

	var chunks []chunkWork
	for start := 0; start < numNodes; start += chunkSize {
		end := start + chunkSize
		if end > numNodes {
			end = numNodes
		}
		chunks = append(chunks, chunkWork{start, end})
	}

	if len(config.System.Workers) == 0 {
		return 0, fmt.Errorf("no workers available for distributed backend")
	}

	resChans := make([]chan *pool.Result, len(chunks))
	for i, chunk := range chunks {
		start, end := chunk.start, chunk.end
		addr := config.System.Workers[i%len(config.System.Workers)]

		shardPtr := unsafe.Pointer(uintptr(graphNodes) + uintptr(start*nodeBytes))
		shardBytes := unsafe.Slice((*byte)(shardPtr), (end-start)*nodeBytes)
		dictCopy := append([]byte(nil), shardBytes...)

		jobFn := func(ctx context.Context) (any, error) {
			packed, callErr := remoteBestFillPacked(addr, dictCopy, end-start, ctxCopy, timeout)
			if callErr != nil {
				return nil, callErr
			}
			return numeric.RebasePackedID(packed, start), nil
		}

		resChans[i] = backend.pool.Schedule(
			fmt.Sprintf("dist-%s-%d", addr, start),
			jobFn,
			pool.WithCircuitBreaker(addr, 3, 5*time.Second, 2),
			pool.WithTTL(5*time.Second),
			pool.WithContext(rctx),
		)
	}

	var fallbackWg sync.WaitGroup
	errCh := make(chan error, len(chunks))

	for i, chunk := range chunks {
		select {
		case <-rctx.Done():
			return 0, rctx.Err()
		case res := <-resChans[i]:
			if res.Error != nil {
				if localBuilder != nil {
					fallbackWg.Add(1)
					// Schedule failure fallback locally
					localFn := func(ctx context.Context) (any, error) {
						start, end := chunk.start, chunk.end
						shardPtr := unsafe.Pointer(uintptr(graphNodes) + uintptr(start*nodeBytes))
						packed, fbErr := localBuilder.Resolve(shardPtr, end-start, contextPtr)
						if fbErr != nil {
							return nil, fbErr
						}
						return numeric.RebasePackedID(packed, start), nil
					}

					fbCh := backend.pool.Schedule(fmt.Sprintf("local-%d", chunk.start), localFn, pool.WithTTL(5*time.Second), pool.WithContext(rctx))
					go func() {
						defer fallbackWg.Done()
						select {
						case <-rctx.Done():
							return
						case fbRes := <-fbCh:
							if fbRes.Error != nil {
								errCh <- fbRes.Error
							} else if v, ok := fbRes.Value.(uint64); ok {
								atomicMaxPacked(&best, v)
							} else {
								console.Debug("distributed: local Resolve returned non-uint64", "type", fmt.Sprintf("%T", fbRes.Value), "value", fbRes.Value)
							}
						}
					}()
				} else {
					errCh <- res.Error
				}
			} else if v, ok := res.Value.(uint64); ok {
				atomicMaxPacked(&best, v)
			} else {
				console.Debug("distributed: remote Resolve returned non-uint64", "type", fmt.Sprintf("%T", res.Value), "value", res.Value)
			}
		}
	}

	fallbackWg.Wait()
	close(errCh)

	if err, ok := <-errCh; ok {
		return 0, err
	}

	return best.Load(), nil
}

func remoteBestFillPacked(
	addr string,
	dictionary []byte,
	numNodes int,
	context []byte,
	timeout time.Duration,
) (uint64, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if deadlineErr := conn.SetDeadline(time.Now().Add(timeout)); deadlineErr != nil {
		return 0, deadlineErr
	}

	enc := capnp.NewEncoder(conn)
	dec := capnp.NewDecoder(conn)

	msg, err := newWorkRequestMessage(dictionary, numNodes, context)
	if err != nil {
		return 0, err
	}
	if err = enc.Encode(msg); err != nil {
		return 0, err
	}

	respMsg, err := dec.Decode()
	if err != nil {
		return 0, err
	}

	packed, code, parseErr := parseWorkResponseMessage(respMsg)
	if parseErr != nil {
		return 0, parseErr
	}
	if code != messageErrNone {
		return 0, fmt.Errorf("remote worker error code=%d", code)
	}

	return packed, nil
}

func StartDistributedWorker(ctx context.Context, addr string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}

	boundAddr := fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	localBuilder := NewBuilder()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			go handleDistributedConn(conn, localBuilder)
		}
	}()

	return boundAddr, nil
}

func handleDistributedConn(conn net.Conn, localBuilder *Builder) {
	defer conn.Close()

	dec := capnp.NewDecoder(conn)
	enc := capnp.NewEncoder(conn)

	for {
		msg, err := dec.Decode()
		if err != nil {
			return
		}

		dict, numNodes, ctxBytes, parseErr := parseWorkRequestMessage(msg)
		if parseErr != nil {
			resp, _ := newWorkResponseMessage(0, messageErrInvalid)
			_ = enc.Encode(resp)
			continue
		}

		if len(dict) == 0 || len(ctxBytes) == 0 {
			resp, _ := newWorkResponseMessage(0, messageErrInvalid)
			_ = enc.Encode(resp)
			continue
		}

		packed, runErr := localBuilder.Resolve(
			unsafe.Pointer(&dict[0]),
			numNodes,
			unsafe.Pointer(&ctxBytes[0]),
		)
		if runErr != nil {
			resp, _ := newWorkResponseMessage(0, messageErrCompute)
			_ = enc.Encode(resp)
			continue
		}

		resp, _ := newWorkResponseMessage(packed, messageErrNone)
		if err = enc.Encode(resp); err != nil {
			return
		}
	}
}

func newWorkRequestMessage(
	dictionary []byte,
	numNodes int,
	context []byte,
) (*capnp.Message, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return nil, err
	}
	root, err := capnp.NewRootStruct(seg, capnp.ObjectSize{DataSize: messageDataSize, PointerCount: messagePointerCount})
	if err != nil {
		return nil, err
	}

	st := capnp.Struct(root)
	st.SetUint16(0, messageTypeWorkRequest)
	st.SetUint32(4, uint32(numNodes))
	if err = st.SetData(0, dictionary); err != nil {
		return nil, err
	}
	if err = st.SetData(1, context); err != nil {
		return nil, err
	}

	return msg, nil
}

func newWorkResponseMessage(packed uint64, code uint32) (*capnp.Message, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return nil, err
	}
	root, err := capnp.NewRootStruct(seg, capnp.ObjectSize{DataSize: messageDataSize, PointerCount: messagePointerCount})
	if err != nil {
		return nil, err
	}

	st := capnp.Struct(root)
	st.SetUint16(0, messageTypeWorkResponse)
	st.SetUint32(4, code)
	st.SetUint64(8, packed)

	return msg, nil
}

func parseWorkRequestMessage(msg *capnp.Message) ([]byte, int, []byte, error) {
	root, err := msg.Root()
	if err != nil {
		return nil, 0, nil, err
	}
	st := root.Struct()
	if st.Uint16(0) != messageTypeWorkRequest {
		return nil, 0, nil, fmt.Errorf("unexpected message type: %d", st.Uint16(0))
	}

	numNodes := int(st.Uint32(4))

	dict, err := readStructData(st, 0)
	if err != nil {
		return nil, 0, nil, err
	}
	context, err := readStructData(st, 1)
	if err != nil {
		return nil, 0, nil, err
	}

	if len(dict) < numNodes*nodeBytes {
		return nil, 0, nil, fmt.Errorf("invalid dictionary payload")
	}
	if len(context) < nodeBytes {
		return nil, 0, nil, fmt.Errorf("invalid context payload")
	}

	return dict, numNodes, context, nil
}

func parseWorkResponseMessage(msg *capnp.Message) (uint64, uint32, error) {
	root, err := msg.Root()
	if err != nil {
		return 0, 0, err
	}
	st := root.Struct()
	if st.Uint16(0) != messageTypeWorkResponse {
		return 0, 0, fmt.Errorf("unexpected message type: %d", st.Uint16(0))
	}
	return st.Uint64(8), st.Uint32(4), nil
}

func readStructData(st capnp.Struct, idx uint16) ([]byte, error) {
	if !st.HasPtr(idx) {
		return nil, nil
	}
	p, err := st.Ptr(idx)
	if err != nil {
		return nil, err
	}
	data := p.Data()
	if len(data) == 0 {
		return nil, nil
	}
	return append([]byte(nil), data...), nil
}

func atomicMaxPacked(best *atomic.Uint64, packed uint64) {
	for {
		current := best.Load()
		if packed <= current {
			break
		}
		if best.CompareAndSwap(current, packed) {
			break
		}
	}
}

/*
DistributedWithContext adds a context to the machine.
*/
func DistributedWithContext(ctx context.Context) distributedOpts {
	return func(distributed *DistributedBackend) {
		if ctx == nil {
			ctx = context.Background()
		}
		distributed.ctx, distributed.cancel = context.WithCancel(ctx)
	}
}

func DistributedWithPool(pool *pool.Pool) distributedOpts {
	return func(distributed *DistributedBackend) {
		distributed.pool = pool
	}
}

type discoveryRuntime struct {
	ctx context.Context
}

var (
	discoveryMu         sync.Mutex
	discoveryState      *discoveryRuntime
	configuredWorkersMu sync.Mutex
	configuredWorkers   []string
	workersMutex        sync.Mutex
	instanceID          string
)

func init() {
	instanceID = fmt.Sprintf("%x", time.Now().UnixNano())
}

func configuredWorkerSnapshot() []string {
	configuredWorkersMu.Lock()
	defer configuredWorkersMu.Unlock()

	if len(configuredWorkers) == 0 && len(config.System.Workers) > 0 {
		configuredWorkers = append([]string(nil), config.System.Workers...)
	}

	return append([]string(nil), configuredWorkers...)
}

func resetWorkersToConfigured() {
	workers := configuredWorkerSnapshot()

	workersMutex.Lock()
	defer workersMutex.Unlock()
	config.System.Workers = append([]string(nil), workers...)
}

/*
StartDiscovery begins listening for broadcast UDP packets to discover peers,
and broadcasts our own presence on the local network.
It also starts the local worker process so this node is part of the mesh.
*/
func StartDiscovery(ctx context.Context, port string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	discoveryMu.Lock()
	if discoveryState != nil && discoveryState.ctx != nil && discoveryState.ctx.Err() == nil {
		discoveryMu.Unlock()
		return nil
	}
	discoveryState = &discoveryRuntime{ctx: ctx}
	discoveryMu.Unlock()

	go func(active context.Context) {
		<-active.Done()
		discoveryMu.Lock()
		if discoveryState != nil && discoveryState.ctx == active {
			discoveryState = nil
		}
		discoveryMu.Unlock()
	}(ctx)

	resetWorkersToConfigured()

	boundPort, err := StartDistributedWorker(ctx, port)
	if err != nil {
		discoveryMu.Lock()
		if discoveryState != nil && discoveryState.ctx == ctx {
			discoveryState = nil
		}
		discoveryMu.Unlock()
		return err
	}

	addr := fmt.Sprintf("127.0.0.1%s", boundPort)
	addWorker(addr)

	listenAddr, err := net.ResolveUDPAddr("udp4", ":7778")
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return err
	}

	go func() {
		defer conn.Close()
		buf := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			n, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}

			msg := string(buf[:n])
			parts := strings.SplitN(msg, ":", 3)
			if len(parts) == 3 && parts[0] == "SIX_WORKER" {
				peerID := parts[1]
				peerPort := parts[2]
				if peerID != instanceID {
					peerIP := raddr.IP.String()
					if strings.HasPrefix(peerPort, ":") {
						addWorker(fmt.Sprintf("%s%s", peerIP, peerPort))
					} else {
						addWorker(fmt.Sprintf("%s:%s", peerIP, peerPort))
					}
				}
			}
		}
	}()

	go func() {
		addr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:7778")
		if err != nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			bconn, err := net.DialUDP("udp4", nil, addr)
			if err == nil {
				msg := fmt.Sprintf("SIX_WORKER:%s:%s", instanceID, boundPort)
				_, _ = bconn.Write([]byte(msg))
				_ = bconn.Close()
			}
			time.Sleep(5 * time.Second)
		}
	}()

	return nil
}

func addWorker(addr string) {
	workersMutex.Lock()
	defer workersMutex.Unlock()
	for _, w := range config.System.Workers {
		if w == addr {
			return
		}
	}
	config.System.Workers = append(config.System.Workers, addr)
	console.Debug("Discovered peer", "addr", addr)
}

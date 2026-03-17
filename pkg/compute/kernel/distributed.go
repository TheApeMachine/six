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
	"github.com/theapemachine/six/pkg/errnie"
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
	state := errnie.NewState("kernel/distributed/new")
	backend := &DistributedBackend{}

	for _, opt := range opts {
		opt(backend)
	}

	errnie.GuardVoid(state, func() error {
		if backend.pool == nil {
			return fmt.Errorf("DistributedWithPool must be provided")
		}
		return nil
	})

	return backend, state.Err()
}

func (backend *DistributedBackend) Available() bool {
	return backend != nil && backend.pool != nil && len(config.System.Workers) > 0
}

func (backend *DistributedBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	contextPtr unsafe.Pointer,
) (uint64, error) {
	state := errnie.NewState("kernel/distributed/resolve")

	errnie.GuardVoid(state, func() error {
		if backend == nil || backend.pool == nil {
			return fmt.Errorf("distributed backend unavailable")
		}
		if numNodes <= 0 {
			return fmt.Errorf("invalid numNodes: must be > 0")
		}
		if graphNodes == nil || contextPtr == nil {
			return fmt.Errorf("invalid distributed pointers")
		}
		return nil
	})

	if state.Failed() {
		return 0, state.Err()
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

	errnie.GuardVoid(state, func() error {
		if len(config.System.Workers) == 0 {
			return fmt.Errorf("no workers available for distributed backend")
		}
		return nil
	})

	if state.Failed() {
		return 0, state.Err()
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
		if state.Failed() {
			break
		}

		select {
		case <-rctx.Done():
			errnie.GuardVoid(state, func() error { return rctx.Err() })
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
					errnie.GuardVoid(state, func() error { return res.Error })
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

	errnie.GuardVoid(state, func() error {
		if err, ok := <-errCh; ok {
			return err
		}
		return nil
	})

	return best.Load(), state.Err()
}

func remoteBestFillPacked(
	addr string,
	dictionary []byte,
	numNodes int,
	context []byte,
	timeout time.Duration,
) (uint64, error) {
	state := errnie.NewState("kernel/distributed/remote-best-fill")

	conn := errnie.Guard(state, func() (net.Conn, error) {
		return net.DialTimeout("tcp", addr, timeout)
	})

	if state.Failed() {
		return 0, state.Err()
	}
	defer conn.Close()

	errnie.GuardVoid(state, func() error {
		return conn.SetDeadline(time.Now().Add(timeout))
	})

	enc := capnp.NewEncoder(conn)
	dec := capnp.NewDecoder(conn)

	msg := errnie.Guard(state, func() (*capnp.Message, error) {
		return newWorkRequestMessage(dictionary, numNodes, context)
	})

	errnie.GuardVoid(state, func() error {
		return enc.Encode(msg)
	})

	respMsg := errnie.Guard(state, func() (*capnp.Message, error) {
		return dec.Decode()
	})

	var (
		packed uint64
		code   uint32
	)

	errnie.GuardVoid(state, func() (err error) {
		packed, code, err = parseWorkResponseMessage(respMsg)
		return
	})

	errnie.GuardVoid(state, func() error {
		if code != messageErrNone {
			return fmt.Errorf("remote worker error code=%d", code)
		}
		return nil
	})

	return packed, state.Err()
}

func StartDistributedWorker(ctx context.Context, addr string) (string, error) {
	state := errnie.NewState("kernel/distributed/worker-start")

	if ctx == nil {
		ctx = context.Background()
	}

	ln := errnie.Guard(state, func() (net.Listener, error) {
		return net.Listen("tcp", addr)
	})

	if state.Failed() {
		return "", state.Err()
	}

	boundAddr := fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	localBuilder := NewBuilder()

	go func() {
		acceptState := errnie.NewState("kernel/distributed/accept")
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				if ctx.Err() != nil {
					return
				}
				acceptState.Handle(acceptErr)
				acceptState.Reset()
				continue
			}

			go handleDistributedConn(conn, localBuilder)
		}
	}()

	return boundAddr, nil
}

func handleDistributedConn(conn net.Conn, localBuilder *Builder) {
	state := errnie.NewState("kernel/distributed/handler")
	defer conn.Close()

	dec := capnp.NewDecoder(conn)
	enc := capnp.NewEncoder(conn)

	for {
		state.Reset()

		msg := errnie.Guard(state, func() (*capnp.Message, error) {
			return dec.Decode()
		})

		if state.Failed() {
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
		errnie.GuardVoid(state, func() error {
			return enc.Encode(resp)
		})

		if state.Failed() {
			return
		}
	}
}

func newWorkRequestMessage(
	dictionary []byte,
	numNodes int,
	context []byte,
) (*capnp.Message, error) {
	state := errnie.NewState("kernel/distributed/request")
	var (
		msg *capnp.Message
		seg *capnp.Segment
	)

	errnie.GuardVoid(state, func() (err error) {
		msg, seg, err = capnp.NewMessage(capnp.SingleSegment(nil))
		return
	})

	root := errnie.Guard(state, func() (capnp.Struct, error) {
		return capnp.NewRootStruct(seg, capnp.ObjectSize{
			DataSize:     messageDataSize,
			PointerCount: messagePointerCount,
		})
	})

	st := capnp.Struct(root)
	st.SetUint16(0, messageTypeWorkRequest)
	st.SetUint32(4, uint32(numNodes))

	errnie.GuardVoid(state, func() error {
		return st.SetData(0, dictionary)
	})

	errnie.GuardVoid(state, func() error {
		return st.SetData(1, context)
	})

	return msg, state.Err()
}

func newWorkResponseMessage(packed uint64, code uint32) (*capnp.Message, error) {
	state := errnie.NewState("kernel/distributed/response")
	var (
		msg *capnp.Message
		seg *capnp.Segment
	)

	errnie.GuardVoid(state, func() (err error) {
		msg, seg, err = capnp.NewMessage(capnp.SingleSegment(nil))
		return
	})

	root := errnie.Guard(state, func() (capnp.Struct, error) {
		return capnp.NewRootStruct(seg, capnp.ObjectSize{
			DataSize:     messageDataSize,
			PointerCount: messagePointerCount,
		})
	})

	st := capnp.Struct(root)
	st.SetUint16(0, messageTypeWorkResponse)
	st.SetUint32(4, code)
	st.SetUint64(8, packed)

	return msg, state.Err()
}

func parseWorkRequestMessage(msg *capnp.Message) ([]byte, int, []byte, error) {
	state := errnie.NewState("kernel/distributed/parse-request")

	root := errnie.Guard(state, func() (capnp.Ptr, error) {
		return msg.Root()
	})

	st := root.Struct()
	errnie.GuardVoid(state, func() error {
		if st.Uint16(0) != messageTypeWorkRequest {
			return fmt.Errorf("unexpected message type: %d", st.Uint16(0))
		}
		return nil
	})

	numNodes := int(st.Uint32(4))

	dict := errnie.Guard(state, func() ([]byte, error) {
		return readStructData(st, 0)
	})

	context := errnie.Guard(state, func() ([]byte, error) {
		return readStructData(st, 1)
	})

	errnie.GuardVoid(state, func() error {
		if len(dict) < numNodes*nodeBytes {
			return fmt.Errorf("invalid dictionary payload")
		}
		if len(context) < nodeBytes {
			return fmt.Errorf("invalid context payload")
		}
		return nil
	})

	return dict, numNodes, context, state.Err()
}

func parseWorkResponseMessage(msg *capnp.Message) (uint64, uint32, error) {
	state := errnie.NewState("kernel/distributed/parse-response")

	root := errnie.Guard(state, func() (capnp.Ptr, error) {
		return msg.Root()
	})

	st := root.Struct()
	errnie.GuardVoid(state, func() error {
		if st.Uint16(0) != messageTypeWorkResponse {
			return fmt.Errorf("unexpected message type: %d", st.Uint16(0))
		}
		return nil
	})

	return st.Uint64(8), st.Uint32(4), state.Err()
}

func readStructData(st capnp.Struct, idx uint16) ([]byte, error) {
	state := errnie.NewState("kernel/distributed/read-struct")

	if !st.HasPtr(idx) {
		return nil, nil
	}

	p := errnie.Guard(state, func() (capnp.Ptr, error) {
		return st.Ptr(idx)
	})

	if state.Failed() {
		return nil, state.Err()
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
	state := errnie.NewState("kernel/distributed/discovery")

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

	boundPort := errnie.Guard(state, func() (string, error) {
		return StartDistributedWorker(ctx, port)
	})

	if state.Failed() {
		discoveryMu.Lock()
		if discoveryState != nil && discoveryState.ctx == ctx {
			discoveryState = nil
		}
		discoveryMu.Unlock()
		return state.Err()
	}

	addr := fmt.Sprintf("127.0.0.1%s", boundPort)
	addWorker(addr)

	listenAddr := errnie.Guard(state, func() (*net.UDPAddr, error) {
		return net.ResolveUDPAddr("udp4", ":7778")
	})

	conn := errnie.Guard(state, func() (*net.UDPConn, error) {
		return net.ListenUDP("udp4", listenAddr)
	})

	if state.Failed() {
		return state.Err()
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
		broadcastState := errnie.NewState("kernel/distributed/broadcast")
		addr := errnie.Guard(broadcastState, func() (*net.UDPAddr, error) {
			return net.ResolveUDPAddr("udp4", "255.255.255.255:7778")
		})

		if broadcastState.Failed() {
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

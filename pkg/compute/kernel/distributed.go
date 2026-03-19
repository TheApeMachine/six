package kernel

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"capnproto.org/go/capnp/v3"
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
			return errnie.Error(
				NewDistributedError(DistributedErrorPoolRequired),
			)
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
			return errnie.Error(
				NewDistributedError(DistributedErrorBackendUnavailable),
			)
		}

		if numNodes <= 0 {
			return errnie.Error(
				NewDistributedError(DistributedErrorInvalidNumNodes),
			)
		}

		if graphNodes == nil || contextPtr == nil {
			return errnie.Error(
				NewDistributedError(
					DistributedErrorInvalidDistributedPointers,
				),
			)
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

	timeout := distributedTimeout()
	rctx, cancel := context.WithTimeout(resolveCtx, timeout)
	defer cancel()

	contextBytes := unsafe.Slice((*byte)(contextPtr), nodeBytes)

	chunkSize := max(config.System.Chunk, config.Numeric.VocabSize)
	var best uint64

	type chunkWork struct {
		start int
		end   int
	}

	var chunks []chunkWork
	for start := 0; start < numNodes; start += chunkSize {
		end := min(start+chunkSize, numNodes)
		chunks = append(chunks, chunkWork{start, end})
	}

	errnie.GuardVoid(state, func() error {
		if len(config.System.Workers) == 0 {
			return errnie.Error(
				NewDistributedError(DistributedErrorNoWorkersAvailable),
			)
		}

		return nil
	})

	if state.Failed() {
		return 0, state.Err()
	}

	type scheduledChunk struct {
		start    int
		end      int
		addr     string
		resultCh chan *pool.Result
	}

	scheduled := make([]scheduledChunk, 0, len(chunks))

	for i, chunk := range chunks {
		start, end := chunk.start, chunk.end
		addr := config.System.Workers[i%len(config.System.Workers)]

		jobFn := func(ctx context.Context) (any, error) {
			shardPtr := unsafe.Add(graphNodes, (start * nodeBytes))
			shardBytes := unsafe.Slice((*byte)(shardPtr), (end-start)*nodeBytes)

			packed, callErr := remoteBestFillPacked(
				addr, shardBytes, end-start, contextBytes, timeout,
			)

			if callErr != nil {
				return nil, callErr
			}

			return numeric.RebasePackedID(packed, start), nil
		}

		resultCh := backend.pool.Schedule(
			fmt.Sprintf("dist-%s-%d", addr, start),
			jobFn,
			pool.WithCircuitBreaker(
				addr,
				config.System.MaxFailures,
				time.Duration(
					config.System.FailureTimeout,
				)*time.Second,
				config.System.FailureBackoff,
			),
			pool.WithTTL(
				time.Duration(config.System.FailureTimeout)*time.Second,
			),
			pool.WithContext(rctx),
		)

		scheduled = append(scheduled, scheduledChunk{
			start:    start,
			end:      end,
			addr:     addr,
			resultCh: resultCh,
		})
	}

	for _, chunk := range scheduled {
		select {
		case <-rctx.Done():
			return 0, rctx.Err()
		case res := <-chunk.resultCh:
			if res == nil {
				return 0, errnie.Error(
					NewDistributedError(DistributedErrorChunkResultNil),
					"chunk", chunk.start, "end", chunk.end, "addr", chunk.addr,
				)
			}

			if res.Error != nil {
				return 0, errnie.Error(
					NewDistributedError(DistributedErrorChunkResultError),
					"chunk", chunk.start, "end", chunk.end, "addr", chunk.addr,
				)
			}

			value, ok := res.Value.(uint64)
			if !ok {
				return 0, errnie.Error(
					NewDistributedError(DistributedErrorChunkResultNonUint64),
					"chunk", chunk.start, "end", chunk.end, "addr", chunk.addr,
				)
			}

			best = max(best, value)
		}
	}

	return best, nil
}

/*
distributedTimeout returns the configured distributed timeout with sane defaulting.
*/
func distributedTimeout() time.Duration {
	timeout := time.Duration(config.System.Timeout) * time.Millisecond
	if timeout <= 0 {
		return 5 * time.Second
	}
	return timeout
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
			return errnie.Error(
				NewDistributedError(DistributedErrorRemoteWorkerError),
				"code", code,
			)
		}
		return nil
	})

	return packed, state.Err()
}

func StartDistributedWorker(ctx context.Context) (string, error) {
	state := errnie.NewState("kernel/distributed/worker-start")

	if ctx == nil {
		ctx = context.Background()
	}

	ln := errnie.Guard(state, func() (net.Listener, error) {
		return net.Listen("tcp", ":0")
	})

	if state.Failed() {
		return "", state.Err()
	}

	boundAddr := fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port)
	localBuilder := NewBuilder()
	workerPool := pool.New(ctx, 2, max(2, runtime.NumCPU()), &pool.Config{})

	workerPool.Schedule(
		"kernel/distributed/worker-close-listener",
		func(active context.Context) (any, error) {
			<-active.Done()
			return nil, ln.Close()
		},
		pool.WithContext(ctx),
		pool.WithTTL(time.Second),
	)

	workerPool.Schedule(
		"kernel/distributed/worker-accept",
		func(active context.Context) (any, error) {
			acceptState := errnie.NewState("kernel/distributed/accept")
			for {
				conn, acceptErr := ln.Accept()
				if acceptErr != nil {
					if ctx.Err() != nil {
						return nil, nil
					}
					acceptState.Handle(acceptErr)
					acceptState.Reset()
					continue
				}

				workerPool.Schedule(
					fmt.Sprintf("kernel/distributed/worker-conn-%d", time.Now().UnixNano()),
					func(runCtx context.Context) (any, error) {
						handleDistributedConn(conn, localBuilder)
						return nil, nil
					},
					pool.WithContext(ctx),
					pool.WithTTL(time.Second),
				)
			}
		},
		pool.WithContext(ctx),
		pool.WithTTL(time.Second),
	)

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
			return errnie.Error(
				NewDistributedError(DistributedErrorUnexpectedMessageType),
				"type", st.Uint16(0),
			)
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
			return errnie.Error(
				NewDistributedError(DistributedErrorInvalidDictionaryPayload),
				"length", len(dict),
				"expected", numNodes*nodeBytes,
			)
		}
		if len(context) < nodeBytes {
			return errnie.Error(
				NewDistributedError(DistributedErrorInvalidContextPayload),
				"length", len(context),
				"expected", nodeBytes,
			)
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

	return data, nil
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
func StartDiscovery(ctx context.Context) error {
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

	discoveryPool := pool.New(ctx, 3, max(3, runtime.NumCPU()), &pool.Config{})

	discoveryPool.Schedule(
		"kernel/distributed/discovery-cleanup",
		func(active context.Context) (any, error) {
			<-active.Done()
			discoveryMu.Lock()
			if discoveryState != nil && discoveryState.ctx == active {
				discoveryState = nil
			}
			discoveryMu.Unlock()
			return nil, nil
		},
		pool.WithContext(ctx),
		pool.WithTTL(time.Second),
	)

	resetWorkersToConfigured()

	boundPort := errnie.Guard(state, func() (string, error) {
		return StartDistributedWorker(ctx)
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

	discoveryPool.Schedule(
		"kernel/distributed/discovery-listen",
		func(active context.Context) (any, error) {
			defer conn.Close()
			buf := make([]byte, 1024)
			for {
				select {
				case <-ctx.Done():
					return nil, nil
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
		},
		pool.WithContext(ctx),
		pool.WithTTL(time.Second),
	)

	discoveryPool.Schedule(
		"kernel/distributed/discovery-broadcast",
		func(active context.Context) (any, error) {
			broadcastState := errnie.NewState("kernel/distributed/broadcast")
			addr := errnie.Guard(broadcastState, func() (*net.UDPAddr, error) {
				return net.ResolveUDPAddr("udp4", "255.255.255.255:7778")
			})

			if broadcastState.Failed() {
				return nil, broadcastState.Err()
			}

			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return nil, nil
				case <-ticker.C:
				}

				bconn, err := net.DialUDP("udp4", nil, addr)
				if err == nil {
					msg := fmt.Sprintf("SIX_WORKER:%s:%s", instanceID, boundPort)
					_, _ = bconn.Write([]byte(msg))
					_ = bconn.Close()
				}
			}
		},
		pool.WithContext(ctx),
		pool.WithTTL(time.Second),
	)

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

type DistributedErrorType string

const (
	DistributedErrorPoolRequired               DistributedErrorType = "pool required"
	DistributedErrorBackendUnavailable         DistributedErrorType = "backend unavailable"
	DistributedErrorInvalidDistributedPointers DistributedErrorType = "invalid distributed pointers"
	DistributedErrorInvalidNumNodes            DistributedErrorType = "invalid numNodes"
	DistributedErrorInvalidDictionaryPayload   DistributedErrorType = "invalid dictionary payload"
	DistributedErrorInvalidContextPayload      DistributedErrorType = "invalid context payload"
	DistributedErrorRemoteWorkerError          DistributedErrorType = "remote worker error"
	DistributedErrorRemoteWorkerTimeout        DistributedErrorType = "remote worker timeout"
	DistributedErrorRemoteWorkerCanceled       DistributedErrorType = "remote worker canceled"
	DistributedErrorRemoteWorkerPanic          DistributedErrorType = "remote worker panic"
	DistributedErrorNoWorkersAvailable         DistributedErrorType = "no workers available"
	DistributedErrorChunkResultNil             DistributedErrorType = "chunk result nil"
	DistributedErrorChunkResultError           DistributedErrorType = "chunk result error"
	DistributedErrorChunkResultNonUint64       DistributedErrorType = "chunk result non-uint64"
	DistributedErrorUnexpectedMessageType      DistributedErrorType = "unexpected message type"
)

type DistributedError struct {
	Message string
	Err     DistributedErrorType
}

func NewDistributedError(err DistributedErrorType) *DistributedError {
	return &DistributedError{
		Message: string(err),
		Err:     err,
	}
}

func (err DistributedError) Error() string {
	return fmt.Sprintf("distributed error: %s: %s", err.Message, err.Err)
}

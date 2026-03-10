package kernel

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"capnproto.org/go/capnp/v3"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/numeric"
	"github.com/theapemachine/six/pool"
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

func NewDistributedBackend(opts ...distributedOpts) *DistributedBackend {
	backend := &DistributedBackend{}

	for _, opt := range opts {
		opt(backend)
	}

	if backend.pool == nil {
		panic("DistributedBackend: DistributedWithPool must be provided")
	}

	return backend
}

func (backend *DistributedBackend) Available() bool {
	return true
}

func (backend *DistributedBackend) Resolve(
	graphNodes unsafe.Pointer,
	numNodes int,
	contextPtr unsafe.Pointer,
) (uint64, error) {
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

	chunkSize := config.System.Chunk
	if chunkSize < 256 {
		chunkSize = 256
	}
	timeout := time.Duration(config.System.Timeout) * time.Millisecond
	remoteOnly := config.System.RemoteOnly

	var best atomic.Uint64
	var localBuilder *Builder

	if !remoteOnly {
		localBuilder = &Builder{}
		bak := config.System.Backend
		if bak == "distributed" {
			config.System.Backend = "cpu"
		}
		localBuilder = NewBuilder()
		config.System.Backend = bak
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

	resChans := make([]chan *pool.Result, len(chunks))
	for i, chunk := range chunks {
		start, end := chunk.start, chunk.end
		addr := config.System.Workers[i%len(config.System.Workers)]

		shardPtr := unsafe.Pointer(uintptr(graphNodes) + uintptr(start*nodeBytes))
		shardBytes := unsafe.Slice((*byte)(shardPtr), (end-start)*nodeBytes)
		dictCopy := append([]byte(nil), shardBytes...)

		jobFn := func() (any, error) {
			packed, callErr := remoteBestFillPacked(addr, dictCopy, end-start, ctxCopy, timeout)
			if callErr != nil {
				return nil, callErr
			}
			return numeric.RebasePackedID(packed, start), nil
		}

		resChans[i] = backend.pool.Schedule(
			fmt.Sprintf("dist-%s-%d", addr, start),
			jobFn,
			pool.WithCircuitBreaker(addr, 3, 5*time.Second),
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
				localFn := func() (any, error) {
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
							fmt.Printf("distributed: local Resolve returned non-uint64: %T\n", fbRes.Value)
						}
					}
				}()
			} else {
				errCh <- res.Error
			}
		} else if v, ok := res.Value.(uint64); ok {
			atomicMaxPacked(&best, v)
		} else {
			fmt.Printf("distributed: remote Resolve returned non-uint64: %T\n", res.Value)
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

func StartDistributedWorker(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	bak := config.System.Backend
	// So we don't accidentally recursively use the distributed backend!
	if bak == "distributed" || bak == "" {
		config.System.Backend = "cpu"
	}
	localBuilder := NewBuilder()
	config.System.Backend = bak // Restore

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}

		go handleDistributedConn(conn, localBuilder)
	}
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
		distributed.ctx, distributed.cancel = context.WithCancel(ctx)
	}
}

func DistributedWithPool(pool *pool.Pool) distributedOpts {
	return func(distributed *DistributedBackend) {
		distributed.pool = pool
	}
}

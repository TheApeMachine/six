package kernel

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"capnproto.org/go/capnp/v3"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/kernel/cpu"
	"github.com/theapemachine/six/numeric"
)

const (
	messageTypeWorkRequest  = 11
	messageTypeWorkResponse = 12

	messageErrNone      = 0
	messageErrInvalid   = 1
	messageErrCompute   = 2
	messageDataSize     = 32
	messagePointerCount = 5
	precisionBytes      = 5 * 27 * 2
)

func distributedWorkersFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("SIX_DISTRIBUTED_WORKERS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("SIX_WORKERS"))
	}
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	workers := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		addr := strings.TrimSpace(part)
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		workers = append(workers, addr)
	}
	return workers
}

func bestFillDistributedPacked(
	workers []string,
	dictionary unsafe.Pointer,
	numChords int,
	contextPtr unsafe.Pointer,
	expectedPtr unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) (uint64, error) {
	if len(workers) == 0 {
		return 0, fmt.Errorf("no distributed workers configured")
	}
	if dictionary == nil || contextPtr == nil {
		return 0, fmt.Errorf("invalid distributed pointers")
	}

	if expectedPtr == nil {
		expectedPtr = contextPtr
	}

	contextBytes, err := numeric.PtrToBytes(contextPtr, numeric.ManifoldBytes)
	if err != nil {
		return 0, err
	}
	expectedBytes, err := numeric.PtrToBytes(expectedPtr, numeric.ManifoldBytes)
	if err != nil {
		return 0, err
	}

	var lutBytes []byte
	var precisionData []byte
	if geodesicLUT != nil {
		lutBytes, err = numeric.PtrToBytes(geodesicLUT, numeric.GeodesicMatrixSize)
		if err != nil {
			return 0, err
		}
	}
	if expectedPrecision != nil {
		precisionData, err = numeric.PtrToBytes(expectedPrecision, precisionBytes)
		if err != nil {
			return 0, err
		}
	}

	ctxCopy := append([]byte(nil), contextBytes...)
	expCopy := append([]byte(nil), expectedBytes...)
	precisionCopy := append([]byte(nil), precisionData...)
	lutCopy := append([]byte(nil), lutBytes...)

	chunkSize := config.System.Chunk
	if chunkSize < 256 {
		chunkSize = 256
	}
	timeout := time.Duration(config.System.Timeout) * time.Millisecond
	remoteOnly := config.System.RemoteOnly

	var next atomic.Int64
	var best atomic.Uint64
	var wg sync.WaitGroup
	errCh := make(chan error, len(workers)+1)

	for _, addr := range workers {
		addr := addr
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				start := int(next.Add(int64(chunkSize)) - int64(chunkSize))
				if start >= numChords {
					return
				}
				end := min(start+chunkSize, numChords)
				shardPtr := unsafe.Pointer(uintptr(dictionary) + uintptr(start*numeric.ManifoldBytes))
				shardBytes := unsafe.Slice((*byte)(shardPtr), (end-start)*numeric.ManifoldBytes)
				dictCopy := append([]byte(nil), shardBytes...)

				packed, callErr := remoteBestFillPacked(addr, dictCopy, end-start, ctxCopy, expCopy, precisionCopy, lutCopy, timeout)
				if callErr != nil {
					fallbackPacked, fbErr := cpu.BestFillCPUPackedBytes(dictCopy, end-start, ctxCopy, expCopy, precisionCopy, lutCopy)
					if fbErr != nil {
						errCh <- callErr
						return
					}
					packed = fallbackPacked
				}

				packed = numeric.RebasePackedID(packed, start)
				atomicMaxPacked(&best, packed)
			}
		}()
	}

	if !remoteOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				start := int(next.Add(int64(chunkSize)) - int64(chunkSize))
				if start >= numChords {
					return
				}
				end := min(start+chunkSize, numChords)
				shardPtr := unsafe.Pointer(uintptr(dictionary) + uintptr(start*numeric.ManifoldBytes))
				packed, runErr := BestFillLocalPacked(shardPtr, end-start, contextPtr, expectedPtr, expectedPrecision, geodesicLUT)
				if runErr != nil {
					errCh <- runErr
					return
				}
				packed = numeric.RebasePackedID(packed, start)
				atomicMaxPacked(&best, packed)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	if err, ok := <-errCh; ok {
		return 0, err
	}

	return best.Load(), nil
}

func remoteBestFillPacked(
	addr string,
	dictionary []byte,
	numChords int,
	context []byte,
	expected []byte,
	precision []byte,
	geodesicLUT []byte,
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

	msg, err := newWorkRequestMessage(dictionary, numChords, context, expected, precision, geodesicLUT)
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

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}

		go handleDistributedConn(conn)
	}
}

func handleDistributedConn(conn net.Conn) {
	defer conn.Close()

	dec := capnp.NewDecoder(conn)
	enc := capnp.NewEncoder(conn)

	for {
		msg, err := dec.Decode()
		if err != nil {
			return
		}

		dict, numChords, ctxBytes, expBytes, precisionBytes, lutBytes, parseErr := parseWorkRequestMessage(msg)
		if parseErr != nil {
			resp, _ := newWorkResponseMessage(0, messageErrInvalid)
			_ = enc.Encode(resp)
			continue
		}

		packed, runErr := BestFillLocalPacked(
			numeric.FirstPtr(dict),
			numChords,
			numeric.FirstPtr(ctxBytes),
			numeric.FirstPtr(expBytes),
			numeric.FirstPtr(precisionBytes),
			numeric.FirstPtr(lutBytes),
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
	numChords int,
	context []byte,
	expected []byte,
	precision []byte,
	geodesicLUT []byte,
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
	st.SetUint32(4, uint32(numChords))
	if err = st.SetData(0, dictionary); err != nil {
		return nil, err
	}
	if err = st.SetData(1, context); err != nil {
		return nil, err
	}
	if err = st.SetData(2, expected); err != nil {
		return nil, err
	}
	if err = st.SetData(3, precision); err != nil {
		return nil, err
	}
	if err = st.SetData(4, geodesicLUT); err != nil {
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

func parseWorkRequestMessage(msg *capnp.Message) ([]byte, int, []byte, []byte, []byte, []byte, error) {
	root, err := msg.Root()
	if err != nil {
		return nil, 0, nil, nil, nil, nil, err
	}
	st := root.Struct()
	if st.Uint16(0) != messageTypeWorkRequest {
		return nil, 0, nil, nil, nil, nil, fmt.Errorf("unexpected message type: %d", st.Uint16(0))
	}

	numChords := int(st.Uint32(4))

	dict, err := readStructData(st, 0)
	if err != nil {
		return nil, 0, nil, nil, nil, nil, err
	}
	context, err := readStructData(st, 1)
	if err != nil {
		return nil, 0, nil, nil, nil, nil, err
	}
	expected, err := readStructData(st, 2)
	if err != nil {
		return nil, 0, nil, nil, nil, nil, err
	}
	precision, err := readStructData(st, 3)
	if err != nil {
		return nil, 0, nil, nil, nil, nil, err
	}
	lut, err := readStructData(st, 4)
	if err != nil {
		return nil, 0, nil, nil, nil, nil, err
	}

	if len(dict) < numChords*numeric.ManifoldBytes {
		return nil, 0, nil, nil, nil, nil, fmt.Errorf("invalid dictionary payload")
	}

	if len(context) < numeric.ManifoldBytes || len(expected) < numeric.ManifoldBytes {
		return nil, 0, nil, nil, nil, nil, fmt.Errorf("invalid context or expected payload")
	}
	if len(precision) > 0 && len(precision) < precisionBytes {
		return nil, 0, nil, nil, nil, nil, fmt.Errorf("invalid precision payload")
	}
	if len(lut) > 0 && len(lut) < numeric.GeodesicMatrixSize {
		return nil, 0, nil, nil, nil, nil, fmt.Errorf("invalid geodesic payload")
	}

	return dict, numChords, context, expected, precision, lut, nil
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

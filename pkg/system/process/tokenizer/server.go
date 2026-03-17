package tokenizer

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/process/sequencer"
	"github.com/theapemachine/six/pkg/validate"
)

/*
UniversalServer tokenizes raw bytes into Morton keys and streams them
directly into the spatial index. Byte in → key out → insert.
*/
type UniversalServer struct {
	ctx          context.Context
	cancel       context.CancelFunc
	serverSide   net.Conn
	clientSide   net.Conn
	client       Universal
	serverConn   *rpc.Conn
	clientConns  map[string]*rpc.Conn
	pool         *pool.Pool
	seq          *sequencer.Sequitur
	pos          uint32
	stateMu      sync.Mutex
	morton       *data.MortonCoder
	healer       *sequencer.BitwiseHealer
	fragment     []byte
	sequence     [][]byte
	sequences    [][][]byte
	sequenceIDs  []int
	emitted      map[int]bool
	nextSequence int
	spatialIndex lsm.SpatialIndex
}

type universalOpts func(*UniversalServer)

/*
NewUniversalServer instantiates a UniversalServer.
*/
func NewUniversalServer(opts ...universalOpts) *UniversalServer {
	server := &UniversalServer{
		clientConns: map[string]*rpc.Conn{},
		morton:      data.NewMortonCoder(),
		seq:         sequencer.NewSequitur(),
		healer:      sequencer.NewBitwiseHealer(),
		emitted:     map[int]bool{},
	}

	for _, opt := range opts {
		opt(server)
	}

	validate.Require(map[string]any{
		"ctx":  server.ctx,
		"pool": server.pool,
		"seq":  server.seq,
	})

	server.serverSide, server.clientSide = net.Pipe()
	server.client = Universal_ServerToClient(server)

	server.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		server.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server
}

/*
Client returns a Cap'n Proto client connected to this UniversalServer.
*/
func (server *UniversalServer) Client(clientID string) Universal {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe.
*/
func (server *UniversalServer) Close() error {
	if server.serverConn != nil {
		errnie.SafeMustVoid(func() error {
			return server.serverConn.Close()
		})

		server.serverConn = nil
	}

	for clientID, conn := range server.clientConns {
		if conn != nil {
			errnie.SafeMustVoid(func() error {
				return conn.Close()
			})
		}

		delete(server.clientConns, clientID)
	}

	if server.serverSide != nil {
		errnie.SafeMustVoid(func() error {
			return server.serverSide.Close()
		})

		server.serverSide = nil
	}

	if server.clientSide != nil {
		errnie.SafeMustVoid(func() error {
			return server.clientSide.Close()
		})

		server.clientSide = nil
	}

	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Write implements Universal_Server. Bytes are buffered into sequencer fragments;
healed Morton keys are emitted only when the surrounding sequence is finalized.
*/
func (server *UniversalServer) Write(ctx context.Context, call Universal_write) error {
	if !server.spatialIndex.IsValid() {
		return UniversalError(ErrNoIndex)
	}

	server.stateMu.Lock()
	defer server.stateMu.Unlock()

	return server.acceptByte(call.Args().Data())
}

/*
Feedback receives an error correction signal from the GraphServer.
*/
func (server *UniversalServer) Feedback(
	ctx context.Context, call Universal_feedback,
) error {
	return nil
}

/*
Done implements Universal_Server.
*/
func (server *UniversalServer) Done(
	ctx context.Context, call Universal_done,
) error {
	if !server.spatialIndex.IsValid() &&
		(len(server.fragment) > 0 || len(server.sequence) > 0 || len(server.sequences) > 0) {
		return UniversalError(ErrNoIndex)
	}

	server.stateMu.Lock()
	defer server.stateMu.Unlock()

	hasPending := len(server.fragment) > 0 || len(server.sequence) > 0
	var keys []uint64
	var err error

	if hasPending {
		keys, err = server.finalizeSequence(false)
	} else {
		keys, err = server.drainSequences(true)
	}

	// server.stateMu.Unlock() removed as it's now deferred


	if err != nil {
		return err
	}

	results, err := call.AllocResults()
	if err != nil {
		return err
	}

	keyList, err := results.NewKeys(int32(len(keys)))
	if err != nil {
		return err
	}

	for i, key := range keys {
		keyList.Set(i, key)
	}

	return server.writeKeys(ctx, keys)
}

/*
SetDataset implements Universal_Server.
*/
func (server *UniversalServer) SetDataset(
	ctx context.Context, call Universal_setDataset,
) error {
	corpus := errnie.SafeMust(func() (capnp.TextList, error) {
		return call.Args().Corpus()
	})

	strings := make([]string, corpus.Len())

	for i := 0; i < corpus.Len(); i++ {
		strings[i] = errnie.SafeMust(func() (string, error) {
			return corpus.At(i)
		})
	}

	return nil
}

/*
tokenize runs the Sequencer over one byte and returns a Morton key.
*/
func (server *UniversalServer) tokenize(raw uint8) uint64 {
	isBoundary, _, _, _ := server.seq.Analyze(server.pos, raw)
	server.pos++

	if isBoundary {
		server.pos = 0
	}

	return server.morton.Pack(server.pos, raw)
}

func (server *UniversalServer) acceptByte(raw byte) error {
	isBoundary, emitK, _, _ := server.seq.Analyze(server.pos, raw)
	server.pos++
	server.fragment = append(server.fragment, raw)

	if !isBoundary {
		return nil
	}

	if emitK != len(server.fragment) {
		return UniversalError(ErrFragmentDrift)
	}

	server.sequence = append(server.sequence, append([]byte(nil), server.fragment...))
	server.fragment = nil
	server.pos = 0

	return nil
}

func (server *UniversalServer) finalizeSequence(force bool) ([]uint64, error) {
	if len(server.fragment) > 0 {
		isBoundary, emitK, _, _ := server.seq.Flush()
		if !isBoundary || emitK != len(server.fragment) {
			return nil, UniversalError(ErrFragmentDrift)
		}

		server.sequence = append(server.sequence, append([]byte(nil), server.fragment...))
		server.fragment = nil
		server.pos = 0
	}

	if len(server.sequence) > 0 {
		keys, err := server.compactSequenceBuffer()
		if err != nil {
			return nil, err
		}

		server.healer.Write(server.sequenceValues(server.sequence))
		server.sequences = append(server.sequences, server.cloneByteSequence(server.sequence))
		server.sequenceIDs = append(server.sequenceIDs, server.nextSequence)
		server.nextSequence++
		server.sequence = nil

		drained, err := server.drainSequences(force)
		if err != nil {
			return nil, err
		}

		return append(keys, drained...), nil
	}

	return server.drainSequences(force)
}

func (server *UniversalServer) compactSequenceBuffer() ([]uint64, error) {
	if len(server.sequences) < server.healer.Capacity() {
		return nil, nil
	}

	healed := server.healer.Heal()
	if len(healed) != len(server.sequences) {
		return nil, UniversalError(ErrHealerDrift)
	}

	keys, err := server.emitSequence(0, healed[0])
	if err != nil {
		return nil, err
	}

	server.sequences = server.sequences[1:]
	server.sequenceIDs = server.sequenceIDs[1:]

	return keys, nil
}

func (server *UniversalServer) drainSequences(force bool) ([]uint64, error) {
	if len(server.sequences) == 0 {
		return nil, nil
	}

	healed := server.healer.Heal()
	if len(healed) != len(server.sequences) {
		return nil, UniversalError(ErrHealerDrift)
	}

	ready := map[int]bool{}

	if force {
		for i := range healed {
			ready[i] = true
		}
	} else {
		for _, component := range server.healer.Components() {
			if len(component) < 2 {
				continue
			}

			newest := component[0]
			for _, idx := range component[1:] {
				if idx > newest {
					newest = idx
				}
			}

			for _, idx := range component {
				if idx == newest {
					continue
				}

				ready[idx] = true
			}
		}
	}

	keys := []uint64{}

	for localIndex := range healed {
		if !ready[localIndex] {
			continue
		}

		emitted, err := server.emitSequence(localIndex, healed[localIndex])
		if err != nil {
			return nil, err
		}

		keys = append(keys, emitted...)
	}

	return keys, nil
}

func (server *UniversalServer) emitSequence(
	localIndex int,
	healed [][]data.Value,
) ([]uint64, error) {
	sequenceID := server.sequenceIDs[localIndex]
	if server.emitted[sequenceID] {
		return nil, nil
	}

	rawSequence, err := server.rechunkSequence(server.sequences[localIndex], healed)
	if err != nil {
		return nil, err
	}

	keys := server.packSequence(rawSequence)
	server.emitted[sequenceID] = true

	return keys, nil
}

func (server *UniversalServer) rechunkSequence(
	rawSequence [][]byte,
	healed [][]data.Value,
) ([][]byte, error) {
	totalRaw := 0
	totalHealed := 0
	joined := make([]byte, 0)

	for _, fragment := range rawSequence {
		totalRaw += len(fragment)
		joined = append(joined, fragment...)
	}

	for _, fragment := range healed {
		totalHealed += len(fragment)
	}

	if totalRaw != totalHealed {
		return nil, UniversalError(ErrHealedLength)
	}

	rechunked := make([][]byte, 0, len(healed))
	offset := 0

	for _, fragment := range healed {
		length := len(fragment)
		rechunked = append(rechunked, append([]byte(nil), joined[offset:offset+length]...))
		offset += length
	}

	return rechunked, nil
}

func (server *UniversalServer) packSequence(sequence [][]byte) []uint64 {
	keys := make([]uint64, 0)

	for _, fragment := range sequence {
		for idx, symbol := range fragment {
			keys = append(keys, server.morton.Pack(uint32(idx+1), symbol))
		}
	}

	return keys
}

func (server *UniversalServer) sequenceValues(sequence [][]byte) [][]data.Value {
	values := make([][]data.Value, len(sequence))

	for i, fragment := range sequence {
		row := make([]data.Value, 0, len(fragment))

		for _, symbol := range fragment {
			row = append(row, data.BaseValue(symbol))
		}

		values[i] = row
	}

	return values
}

func (server *UniversalServer) cloneByteSequence(sequence [][]byte) [][]byte {
	cloned := make([][]byte, len(sequence))

	for i, fragment := range sequence {
		cloned[i] = append([]byte(nil), fragment...)
	}

	return cloned
}

func (server *UniversalServer) writeKeys(ctx context.Context, keys []uint64) error {
	for _, key := range keys {
		err := server.spatialIndex.Write(
			ctx, func(p lsm.SpatialIndex_write_Params) error {
				p.SetKey(key)

				return nil
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

/*
UniversalWithContext sets a cancellable context on the server.
*/
func UniversalWithContext(ctx context.Context) universalOpts {
	return func(server *UniversalServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
UniversalWithPool injects the shared worker pool.
*/
func UniversalWithPool(workerPool *pool.Pool) universalOpts {
	return func(server *UniversalServer) {
		server.pool = workerPool
	}
}

/*
UniversalWithSpatialIndex connects the tokenizer to a spatial index
so it can stream keys directly on Write.
*/
func UniversalWithSpatialIndex(spatialIndex lsm.SpatialIndex) universalOpts {
	return func(server *UniversalServer) {
		server.spatialIndex = spatialIndex
	}
}

/*
UniversalError is a typed error for UniversalServer failures.
*/
type UniversalError string

const (
	ErrNoIndex       UniversalError = "spatial index capability not yet received"
	ErrFragmentDrift UniversalError = "sequencer fragment length drift"
	ErrHealerDrift   UniversalError = "bitwise healer buffer drift"
	ErrHealedLength  UniversalError = "bitwise healer changed sequence length"
)

/*
Error implements the error interface for UniversalError.
*/
func (err UniversalError) Error() string {
	return string(err)
}

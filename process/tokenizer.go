package process

import (
	"context"
	"errors"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/pool"
)

// spatialInsertMethod is the capnp method descriptor for SpatialIndex.insert.
var spatialInsertMethod = capnp.Method{
	InterfaceID:   0xfdb082e626e1958b,
	MethodID:      0,
	InterfaceName: "store/lsm/spatial_index.capnp:SpatialIndex",
	MethodName:    "insert",
}

/*
TokenizerServer implements the Tokenizer_Server interface.
The SpatialIndex is received as a capnp.Client via the broadcast bus —
no import of the lsm package required.
*/
type TokenizerServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	pool        *pool.Pool
	broadcast   *pool.BroadcastGroup
	rpcConn     *rpc.Conn
	spatialConn capnp.Client
}

type tokenizerOpts func(*TokenizerServer)

/*
NewTokenizerServer instantiates a TokenizerServer.
*/
func NewTokenizerServer(opts ...tokenizerOpts) *TokenizerServer {
	server := &TokenizerServer{}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

/*
Announce exports the server as an RPC bootstrap capability over an in-memory
pipe, then broadcasts the client-side net.Conn so other systems can connect
and resolve the Tokenizer capability via Bootstrap.
*/
func (server *TokenizerServer) Announce() {
	if server.broadcast == nil {
		return
	}

	serverSide, clientSide := net.Pipe()

	client := Tokenizer_ServerToClient(server)

	server.rpcConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(client),
	})

	server.broadcast.Send(&pool.Result{
		Value: pool.PoolValue[net.Conn]{
			Key:   "tokenizer",
			Value: clientSide,
		},
	})
}

/*
Receive implements the vm.System interface.
Picks up the client-side net.Conn from the broadcast bus, creates an rpc.Conn,
and bootstraps the SpatialIndex capnp.Client from it.
*/
func (server *TokenizerServer) Receive(result *pool.Result) {
	if result == nil || result.Value == nil {
		return
	}

	if pv, ok := result.Value.(pool.PoolValue[net.Conn]); ok {
		if pv.Key == "spatial_index" {
			conn := rpc.NewConn(rpc.NewStreamTransport(pv.Value), nil)
			server.rpcConn = conn
			server.spatialConn = conn.Bootstrap(server.ctx)
		}
	}
}

/*
Generate implements the Tokenizer_Server.Generate RPC method.
*/
func (server *TokenizerServer) Generate(ctx context.Context, call Tokenizer_generate) error {
	raw, err := call.Args().Raw()

	if err != nil {
		return err
	}

	return server.generate(ctx, raw)
}

/*
Done implements the Tokenizer_Server.Done RPC method.
*/
func (server *TokenizerServer) Done(ctx context.Context, call Tokenizer_done) error {
	return nil
}

func (server *TokenizerServer) generate(ctx context.Context, raw []byte) error {
	if server.pool == nil {
		return errors.New("tokenizer pool is not configured")
	}

	server.pool.Schedule("tokenizer_generate", func() (any, error) {
		seq := NewSequencer(nil)
		var chunk []byte

		for pos, byteVal := range raw {
			chunk = append(chunk, byteVal)
			isBoundary, _ := seq.Analyze(pos, byteVal)

			if isBoundary {
				server.processChunk(ctx, chunk)
				chunk = nil
			}
		}

		if len(chunk) > 0 {
			server.processChunk(ctx, chunk)
		}

		return nil, nil
	})

	return nil
}

func (server *TokenizerServer) processChunk(ctx context.Context, chunk []byte) {
	if len(chunk) < 2 {
		return
	}

	if !server.spatialConn.IsValid() {
		return
	}

	// Chord(Span) = OR(BaseChord(b₀), BaseChord(b₁), …)
	// Running OR: the chord grows as bytes are added, capturing content.
	// Order is encoded in the Morton key, not the chord.
	// order is encoded in the morton key.
	for i := 0; i < len(chunk); i++ {
		currentByte := chunk[i]
		pos := uint32(i)

		// Build the cumulative chord up to this specific sequence position
		// This replaces traversing edges with discrete state checkpoints
		cumulativeChord, _ := data.BuildChord(chunk[:i+1])

		c0 := cumulativeChord.C0()
		c1 := cumulativeChord.C1()
		c2 := cumulativeChord.C2()
		c3 := cumulativeChord.C3()
		c4 := cumulativeChord.C4()
		c5 := cumulativeChord.C5()
		c6 := cumulativeChord.C6()
		c7 := cumulativeChord.C7()

		_ = server.spatialConn.SendStreamCall(ctx, capnp.Send{
			Method:   spatialInsertMethod,
			ArgsSize: capnp.ObjectSize{DataSize: 0, PointerCount: 1},
			PlaceArgs: func(s capnp.Struct) error {
				// GraphEdge: DataSize=8, PointerCount=1
				edge, err := capnp.NewStruct(s.Segment(), capnp.ObjectSize{DataSize: 8, PointerCount: 1})
				if err != nil {
					return err
				}

				edge.SetUint8(0, currentByte)
				// Right byte is no longer stored in the graph edge morton format
				edge.SetUint32(4, pos)

				// Chord: DataSize=64, PointerCount=0 (8×uint64)
				chord, err := capnp.NewStruct(edge.Segment(), capnp.ObjectSize{DataSize: 64, PointerCount: 0})
				if err != nil {
					return err
				}

				chord.SetUint64(0, c0)
				chord.SetUint64(8, c1)
				chord.SetUint64(16, c2)
				chord.SetUint64(24, c3)
				chord.SetUint64(32, c4)
				chord.SetUint64(40, c5)
				chord.SetUint64(48, c6)
				chord.SetUint64(56, c7)

				if err := edge.SetPtr(0, chord.ToPtr()); err != nil {
					return err
				}

				return s.SetPtr(0, edge.ToPtr())
			},
		})
	}

	sequenceChord, _ := data.BuildChord(chunk)

	if server.broadcast != nil {
		server.broadcast.Send(&pool.Result{
			Value: pool.PoolValue[[]data.Chord]{
				Key:   "prompt",
				Value: []data.Chord{sequenceChord},
			},
		})
	}
}

/*
TokenizerWithContext sets a cancellable context on the server.
*/
func TokenizerWithContext(ctx context.Context) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
TokenizerWithPool injects the shared worker pool.
*/
func TokenizerWithPool(p *pool.Pool) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.pool = p
	}
}

/*
TokenizerWithBroadcast injects the broadcast group.
*/
func TokenizerWithBroadcast(broadcast *pool.BroadcastGroup) tokenizerOpts {
	return func(server *TokenizerServer) {
		server.broadcast = broadcast
	}
}

/*
TokenizerError is a typed error for TokenizerServer failures.
*/
type TokenizerError string

const (
	ErrNoIndex TokenizerError = "spatial index capability not yet received"
)

/*
Error implements the error interface for TokenizerError.
*/
func (tokErr TokenizerError) Error() string {
	return string(tokErr)
}

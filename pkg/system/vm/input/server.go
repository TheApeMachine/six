package input

import (
	context "context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/validate"
)

/*
HoldoutType enumerates the strategies for masking part of a prompt.
*/
type HoldoutType uint

const (
	NONE   HoldoutType = iota
	RIGHT              // mask trailing bytes
	LEFT               // mask leading bytes
	CENTER             // mask middle bytes
	RANDOM             // mask randomly selected bytes
	MATCH              // mask bytes matching a pattern
)

/*
Holdout defines how part of the prompt should be masked before processing.
*/
type Holdout struct {
	Percent int
	Type    HoldoutType
	Match   []byte
}

/*
PrompterServer is a Cap'n Proto server for the Prompter interface.
It applies holdout masking to the raw prompt string and returns the
processed bytes. The Machine is responsible for passing those bytes to
the Tokenizer — PrompterServer has no knowledge of any other server.
*/
type PrompterServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	state       *errnie.State
	serverConn  *rpc.Conn
	clientConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	heldout     Holdout
	connMu      sync.RWMutex
}

/*
prompterOpts are options for PrompterServer.
*/
type prompterOpts func(*PrompterServer)

/*
NewPrompterServer instantiates a PrompterServer.
*/
func NewPrompterServer(opts ...prompterOpts) *PrompterServer {
	server := &PrompterServer{
		clientConns: map[string]*rpc.Conn{},
		state:       errnie.NewState("vm/input/prompterServer"),
	}

	for _, opt := range opts {
		opt(server)
	}

	validate.Require(map[string]any{
		"ctx":    server.ctx,
		"cancel": server.cancel,
	})

	serverSide, clientSide := net.Pipe()
	capability := Prompter_ServerToClient(server)

	server.serverConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(capability),
	})

	server.clientConn = rpc.NewConn(rpc.NewStreamTransport(clientSide), nil)

	return server
}

/*
Client returns a Cap'n Proto client connected to this PrompterServer.
Returns the bootstrap capability from the pre-created client connection.
*/
func (server *PrompterServer) Client(clientID string) capnp.Client {
	server.connMu.Lock()
	defer server.connMu.Unlock()

	server.clientConns[clientID] = server.clientConn
	return server.clientConn.Bootstrap(server.ctx)
}

/*
Load approximates concurrent RPC pressure via active client registrations.
*/
func (server *PrompterServer) Load() int64 {
	server.connMu.RLock()
	defer server.connMu.RUnlock()

	return int64(len(server.clientConns))
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *PrompterServer) Close() error {
	if server.clientConn != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.clientConn.Close()
		})

		server.clientConn = nil
	}

	if server.serverConn != nil {
		errnie.GuardVoid(server.state, func() error {
			return server.serverConn.Close()
		})

		server.serverConn = nil
	}

	server.connMu.Lock()
	for clientID := range server.clientConns {
		delete(server.clientConns, clientID)
	}
	server.connMu.Unlock()

	if server.cancel != nil {
		server.cancel()
	}

	return server.state.Err()
}

/*
Generate implements Prompter_Server. It applies holdout masking to the
incoming message and returns the processed bytes.
*/
func (server *PrompterServer) Generate(
	ctx context.Context, call Prompter_generate,
) error {
	msg := errnie.Guard(server.state, func() (string, error) {
		return call.Args().Msg()
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	processed := server.applyHoldout([]byte(msg))

	res := errnie.Guard(server.state, func() (Prompter_generate_Results, error) {
		return call.AllocResults()
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	return res.SetData(processed)
}

/*
Done implements Prompter_Server.
*/
func (server *PrompterServer) Done(ctx context.Context, call Prompter_done) error {
	return nil
}

/*
applyHoldout masks bytes according to the configured holdout strategy.
*/
func (server *PrompterServer) applyHoldout(src []byte) []byte {
	if server.heldout.Type == NONE || server.heldout.Percent == 0 {
		return src
	}

	out := append([]byte(nil), src...)
	maskLen := len(out) * server.heldout.Percent / 100

	switch server.heldout.Type {
	case RIGHT:
		for i := len(out) - maskLen; i < len(out); i++ {
			out[i] = 0
		}
	case LEFT:
		for i := range maskLen {
			out[i] = 0
		}
	case CENTER:
		start := (len(out) - maskLen) / 2
		for i := start; i < start+maskLen; i++ {
			out[i] = 0
		}
	}

	return out
}

/*
PrompterWithContext sets a cancellable context.
*/
func PrompterWithContext(ctx context.Context) prompterOpts {
	return func(server *PrompterServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
PrompterWithHoldout configures the masking strategy and percentage.
*/
func PrompterWithHoldout(prct int, ht HoldoutType) prompterOpts {
	return func(server *PrompterServer) {
		server.heldout.Percent = prct
		server.heldout.Type = ht
	}
}

/*
PrompterWithMatch sets the byte pattern used by the MATCH holdout strategy.
*/
func PrompterWithMatch(match []byte) prompterOpts {
	return func(server *PrompterServer) {
		server.heldout.Match = match
	}
}

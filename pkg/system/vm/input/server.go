package input

import (
	context "context"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
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
	serverSide  net.Conn
	clientSide  net.Conn
	client      Prompter
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	heldout     Holdout
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
	}

	for _, opt := range opts {
		opt(server)
	}

	validate.Require(map[string]any{
		"ctx":    server.ctx,
		"cancel": server.cancel,
	})

	server.serverSide, server.clientSide = net.Pipe()
	server.client = Prompter_ServerToClient(server)

	server.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		server.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *PrompterServer) Close() error {
	server.serverSide.Close()
	server.clientSide.Close()

	return nil
}

/*
Client returns a Cap'n Proto client connected to this PrompterServer.
*/
func (server *PrompterServer) Client(clientID string) Prompter {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Generate implements Prompter_Server. It applies holdout masking to the
incoming message and returns the processed bytes.
*/
func (server *PrompterServer) Generate(
	ctx context.Context, call Prompter_generate,
) error {
	msg, err := call.Args().Msg()
	if err != nil {
		return err
	}

	processed := server.applyHoldout([]byte(msg))

	res, err := call.AllocResults()
	if err != nil {
		return err
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
		for i := 0; i < maskLen; i++ {
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

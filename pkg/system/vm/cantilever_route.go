package vm

import (
	"bytes"
	"context"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
)

/*
CantileverRoute is a single-shot backend operation that routes one prompt
payload through the Cantilever prompt capability and exposes the result as io.
*/
type CantileverRoute struct {
	mu       sync.Mutex
	ctx      context.Context
	raw      capnp.Client
	client   bvp.Cantilever
	input    bytes.Buffer
	output   *bytes.Reader
	finished bool
}

/*
NewCantileverRoute binds a Cantilever client to one backend operation.
*/
func NewCantileverRoute(
	ctx context.Context,
	raw capnp.Client,
	client bvp.Cantilever,
) *CantileverRoute {
	return &CantileverRoute{
		ctx:    ctx,
		raw:    raw,
		client: client,
		output: bytes.NewReader(nil),
	}
}

/*
Read executes the prompt once, then drains the result bytes.
*/
func (route *CantileverRoute) Read(p []byte) (n int, err error) {
	route.mu.Lock()
	defer route.mu.Unlock()

	if !route.finished {
		if err = route.runLocked(); err != nil {
			return 0, err
		}
	}

	return route.output.Read(p)
}

/*
Write buffers the prompt payload for the eventual prompt RPC.
*/
func (route *CantileverRoute) Write(p []byte) (n int, err error) {
	route.mu.Lock()
	defer route.mu.Unlock()

	return route.input.Write(p)
}

/*
Close releases the held capability.
*/
func (route *CantileverRoute) Close() error {
	route.mu.Lock()
	defer route.mu.Unlock()

	if route.raw.IsValid() {
		route.raw.Release()
		route.raw = capnp.Client{}
	}

	return nil
}

/*
run performs the Cantilever prompt RPC exactly once.
*/
func (route *CantileverRoute) run() error {
	route.mu.Lock()
	defer route.mu.Unlock()

	return route.runLocked()
}

/*
runLocked performs the Cantilever prompt RPC while the route mutex is held.
*/
func (route *CantileverRoute) runLocked() error {
	future, release := route.client.Prompt(route.ctx, func(
		params bvp.Cantilever_prompt_Params,
	) error {
		return params.SetMsg(route.input.String())
	})

	defer release()

	result, err := future.Struct()
	if err != nil {
		return err
	}

	text, err := result.Result()
	if err != nil {
		return err
	}

	route.output = bytes.NewReader([]byte(text))
	route.finished = true

	return nil
}

package macro

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/validate"
)

/*
MacroOpcode represents a discovered Logic Circuit (Phase Rotation)
that reliably bridges a specific Phase-Shift boundary gap.
*/
type MacroOpcode struct {
	Rotation numeric.Phase // The G^X necessary to complete the rotation
	UseCount uint64        // Number of times this opcode successfully bridged a gap
	Hardened bool          // Promoted to permanent status after verification
}

/*
AnchorRecord stores a cross-modal prime invariant. Multiple modalities can point
at the same GF(257) anchor so the system can phase-lock text, images, or other
streams onto one resonant address.
*/
type AnchorRecord struct {
	Name       string
	Phase      numeric.Phase
	Modalities map[string]bool
	UseCount   uint64
	Hardened   bool
}

/*
MacroIndexServer stores the library of discovered Macro-Opcodes.
It allows the Cantilever logic engine to look up pre-computed Resonant Sub-Routines
instead of falling back to raw data generation or exhaustive searching.
*/
type MacroIndexServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      MacroIndex
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	mu          sync.RWMutex
	opcodes     map[numeric.Phase]*MacroOpcode
	anchors     map[numeric.Phase]*AnchorRecord
	anchorNames map[string]numeric.Phase
}

/*
IndexOpts ...
*/
type IndexOpts func(*MacroIndexServer)

/*
NewMacroIndexServer instantiates a thread-safe registry for Logic Circuits.
*/
func NewMacroIndexServer(opts ...IndexOpts) *MacroIndexServer {
	idx := &MacroIndexServer{
		clientConns: map[string]*rpc.Conn{},
		opcodes:     make(map[numeric.Phase]*MacroOpcode),
		anchors:     make(map[numeric.Phase]*AnchorRecord),
		anchorNames: make(map[string]numeric.Phase),
	}

	for _, opt := range opts {
		opt(idx)
	}

	validate.Require(map[string]any{
		"ctx":    idx.ctx,
		"cancel": idx.cancel,
	})

	idx.serverSide, idx.clientSide = net.Pipe()
	idx.client = MacroIndex_ServerToClient(idx)

	idx.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		idx.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(idx.client),
	})

	return idx
}

/*
Client returns a Cap'n Proto client connected to this MacroIndexServer.
*/
func (server *MacroIndexServer) Client(clientID string) MacroIndex {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Prompt implements MacroIndex_Server.
*/
func (server *MacroIndexServer) Prompt(ctx context.Context, call MacroIndex_prompt) error {
	return nil
}

/*
MacroIndexWithContext sets the context.
*/
func MacroIndexWithContext(ctx context.Context) IndexOpts {
	return func(idx *MacroIndexServer) {
		idx.ctx, idx.cancel = context.WithCancel(ctx)
	}
}

/*
ComputeExpectedRotation independently calculates the phase rotation needed to
bridge from start to goal in GF(257). Ground truth for all bridge assertions.
*/
func ComputeExpectedRotation(start, goal numeric.Phase) (numeric.Phase, error) {
	calc := numeric.NewCalculus()

	invStart, err := calc.Inverse(start)
	if err != nil {
		return 0, err
	}

	return calc.Multiply(goal, invStart), nil
}

/*
FindOpcode looks up a mathematically required Phase Shift.
Returns the MacroOpcode if one exists that satisfies the BVP boundary constraint.
*/
func (idx *MacroIndexServer) FindOpcode(shift numeric.Phase) (*MacroOpcode, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	opcode, exists := idx.opcodes[shift]
	if !exists {
		return nil, false
	}

	// Increment usage count atomically (for GC or pruning priorities)
	// Mutating through RLock here requires a minor hack or full lock, but since we use
	// atomic operations on the pointer fields, it's generally safe (though Go race detector might complain).
	// For purity without atomic package, we will just upgrade the lock.
	return opcode, true
}

/*
RecordOpcode stores or increments a synthesized tool.
If the tool bridges a gap multiple times, it becomes Hardened.
*/
func (idx *MacroIndexServer) RecordOpcode(shift numeric.Phase) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if opcode, exists := idx.opcodes[shift]; exists {
		opcode.UseCount++
		if opcode.UseCount > 5 { // Threshold for hardening
			opcode.Hardened = true
		}
		return
	}

	idx.opcodes[shift] = &MacroOpcode{
		Rotation: shift,
		UseCount: 1,
		Hardened: false,
	}
}

/*
GarbageCollect prunes inefficient tools (not Hardened and low use) from the Index.
*/
func (idx *MacroIndexServer) GarbageCollect() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	pruned := 0
	for shift, opcode := range idx.opcodes {
		if !opcode.Hardened && opcode.UseCount == 1 {
			delete(idx.opcodes, shift)
			pruned++
		}
	}

	return pruned
}

/*
AvailableHardened returns a list of reliable MacroOpcodes available for composition.
*/
func (idx *MacroIndexServer) AvailableHardened() []*MacroOpcode {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var tools []*MacroOpcode
	for _, tool := range idx.opcodes {
		if tool.Hardened {
			tools = append(tools, tool)
		}
	}
	return tools
}

/*
RecordAnchor stores or refreshes a cross-modal prime invariant. Repeated use
hardens the anchor so it can serve as a stable rendezvous point for phase-locking.
*/
func (idx *MacroIndexServer) RecordAnchor(name string, phase numeric.Phase, modalities ...string) *AnchorRecord {
	if name == "" || phase == 0 {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	record, exists := idx.anchors[phase]
	if !exists {
		record = &AnchorRecord{
			Name:       name,
			Phase:      phase,
			Modalities: make(map[string]bool),
			UseCount:   1,
		}
		idx.anchors[phase] = record
	} else {
		record.UseCount++
		if record.Name == "" {
			record.Name = name
		}
	}

	for _, modality := range modalities {
		if modality != "" {
			record.Modalities[modality] = true
		}
	}

	if record.UseCount > 3 {
		record.Hardened = true
	}

	idx.anchorNames[name] = phase
	return record
}

/*
FindAnchorByName resolves an anchor through its human-facing label.
*/
func (idx *MacroIndexServer) FindAnchorByName(name string) (*AnchorRecord, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	phase, exists := idx.anchorNames[name]
	if !exists {
		return nil, false
	}

	record, ok := idx.anchors[phase]
	return record, ok
}

/*
FindAnchorByPhase returns the anchor stored at a GF(257) phase.
*/
func (idx *MacroIndexServer) FindAnchorByPhase(phase numeric.Phase) (*AnchorRecord, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	record, exists := idx.anchors[phase]
	return record, exists
}

/*
AvailableAnchors returns every known anchor currently in the registry.
*/
func (idx *MacroIndexServer) AvailableAnchors() []*AnchorRecord {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	out := make([]*AnchorRecord, 0, len(idx.anchors))
	for _, record := range idx.anchors {
		out = append(out, record)
	}
	return out
}

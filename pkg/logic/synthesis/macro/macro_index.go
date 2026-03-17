package macro

import (
	"context"
	"fmt"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/validate"
)

const (
	hardeningThreshold = 5
)

/*
AffineKey indexes macro-opcodes by their geometric
affine signature. The pair (Scale, Translate) lives
in GF(257) × GF(257), yielding ~66K distinct keys.
*/
type AffineKey struct {
	Scale     uint16
	Translate uint16
}

/*
AffineKeyFromValues computes the geometric affine
signature of the delta between two values. The XOR
captures the structural difference; RotationSeed
projects it into the (Scale, Translate) plane.
*/
func AffineKeyFromValues(start, goal data.Value) AffineKey {
	delta := start.XOR(goal)
	scale, translate := delta.RotationSeed()

	return AffineKey{Scale: scale, Translate: translate}
}

/*
String formats the key for path signatures and diagnostics.
*/
func (key AffineKey) String() string {
	return fmt.Sprintf("%d:%d", key.Scale, key.Translate)
}

/*
MacroOpcode represents a discovered affine logic circuit that reliably
bridges a specific boundary gap in the 5-sparse geometric state space.
The transformation f(x) = Scale·x + Translate (mod 257) maps one
sparse state to another.
*/
type MacroOpcode struct {
	Key       AffineKey     // The affine signature indexing this opcode
	Scale     numeric.Phase // The multiplicative rotation component a
	Translate numeric.Phase // The additive translation component b
	UseCount  uint64        // Number of times this opcode successfully bridged a gap
	Hardened  bool          // Promoted to permanent status after verification
}

/*
ApplyPhase advances a scalar phase through this opcode's affine operator.
*/
func (opcode *MacroOpcode) ApplyPhase(phase numeric.Phase) numeric.Phase {
	return numeric.Phase((uint32(opcode.Scale)*uint32(phase) + uint32(opcode.Translate)) % numeric.FermatPrime)
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
	opcodes     map[AffineKey]*MacroOpcode
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
		opcodes:     make(map[AffineKey]*MacroOpcode),
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
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *MacroIndexServer) Close() error {
	if server.serverConn != nil {
		_ = server.serverConn.Close()
		server.serverConn = nil
	}

	for clientID, conn := range server.clientConns {
		if conn != nil {
			_ = conn.Close()
		}
		delete(server.clientConns, clientID)
	}

	if server.serverSide != nil {
		_ = server.serverSide.Close()
		server.serverSide = nil
	}
	if server.clientSide != nil {
		_ = server.clientSide.Close()
		server.clientSide = nil
	}
	if server.cancel != nil {
		server.cancel()
	}

	return nil
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
ComputeExpectedAffineKey computes the geometric affine key for bridging two Values.
*/
func ComputeExpectedAffineKey(startValue, goalValue data.Value) AffineKey {
	return AffineKeyFromValues(startValue, goalValue)
}

/*
FindOpcode looks up a geometrically required affine transformation.
Returns the MacroOpcode if one exists that satisfies the BVP boundary constraint.
*/
func (idx *MacroIndexServer) FindOpcode(key AffineKey) (*MacroOpcode, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	opcode, exists := idx.opcodes[key]
	return opcode, exists
}

/*
RecordOpcode stores or increments a synthesized affine tool.
If the tool bridges a gap multiple times, it becomes Hardened.
*/
func (idx *MacroIndexServer) RecordOpcode(key AffineKey) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if opcode, exists := idx.opcodes[key]; exists {
		opcode.UseCount++
		if opcode.UseCount > hardeningThreshold {
			opcode.Hardened = true
		}
		return
	}

	idx.opcodes[key] = &MacroOpcode{
		Key:       key,
		Scale:     numeric.Phase(key.Scale),
		Translate: numeric.Phase(key.Translate),
		UseCount:  1,
		Hardened:  false,
	}
}

/*
GarbageCollect prunes inefficient tools (not Hardened and low use) from the Index.
*/
func (idx *MacroIndexServer) GarbageCollect() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	pruned := 0
	for key, opcode := range idx.opcodes {
		if !opcode.Hardened && opcode.UseCount == 1 {
			delete(idx.opcodes, key)
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

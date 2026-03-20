package macro

import (
	"context"
	"fmt"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/validate"
)

const (
	hardeningThreshold = 5
)

/*
AffineKey indexes macro-opcodes by their full 5-sparse geometric
affine signature. It encapsulates the complete 8.8-billion-state
space of the GF(257) values rather than downcasting to scalars.
*/
type AffineKey [8]uint64

/*
AffineKeyFromValues computes the exact geometric signature of
the delta between two values. By capturing all 512 bits (8 blocks),
we eliminate catastrophic phase aliasing in the MacroIndex.
*/
func AffineKeyFromValues(start, goal primitive.Value) AffineKey {
	state := errnie.NewState("macro/affineKeyFromValues")

	delta := errnie.Guard(state, func() (primitive.Value, error) {
		return start.XOR(goal)
	})

	return AffineKey{
		delta.Block(0), delta.Block(1), delta.Block(2), delta.Block(3),
		delta.Block(4), delta.Block(5), delta.Block(6), delta.Block(7),
	}
}

/*
String formats the key for path signatures and diagnostics.
*/
func (key AffineKey) String() string {
	return fmt.Sprintf("%016x:%016x...%016x", key[0], key[1], key[7])
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
ProgramCandidate stores a transient system-synthesized program before it is
trusted enough to live as a hardened MacroOpcode.
*/
type ProgramCandidate struct {
	Key          AffineKey
	Scale        numeric.Phase
	Translate    numeric.Phase
	SuccessCount uint64
	FailureCount uint64
	PreResidue   int
	PostResidue  int
	Advanced     bool
	Stable       bool
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
	serverConn  *rpc.Conn
	clientConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	mu          sync.RWMutex
	opcodes     map[AffineKey]*MacroOpcode
	candidates  map[AffineKey]*ProgramCandidate
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
		candidates:  make(map[AffineKey]*ProgramCandidate),
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

	serverSide, clientSide := net.Pipe()
	capability := MacroIndex_ServerToClient(idx)

	idx.serverConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(capability),
	})

	idx.clientConn = rpc.NewConn(rpc.NewStreamTransport(clientSide), nil)

	return idx
}

/*
Client returns a Cap'n Proto client connected to this MacroIndexServer.
Returns the bootstrap capability from the pre-created client connection.
*/
func (server *MacroIndexServer) Client(clientID string) MacroIndex {
	server.clientConns[clientID] = server.clientConn
	return MacroIndex(server.clientConn.Bootstrap(server.ctx))
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *MacroIndexServer) Close() error {
	if server.clientConn != nil {
		_ = server.clientConn.Close()
		server.clientConn = nil
	}

	if server.serverConn != nil {
		_ = server.serverConn.Close()
		server.serverConn = nil
	}

	for clientID := range server.clientConns {
		delete(server.clientConns, clientID)
	}

	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Write implements MacroIndex_Server. It receives a (start, end) Value pair and
records an opcode candidate for the geometric gap between the two boundaries.
*/
func (server *MacroIndexServer) Write(ctx context.Context, call MacroIndex_write) error {
	args := call.Args()

	start, err := args.Start()
	if err != nil {
		return err
	}

	end, err := args.End()
	if err != nil {
		return err
	}

	key := AffineKeyFromValues(start, end)
	server.RecordOpcode(key)

	return nil
}

/*
Done implements MacroIndex_Server. It finalizes the streaming session and
returns summary statistics for the most recently recorded opcode.
*/
func (server *MacroIndexServer) Done(ctx context.Context, call MacroIndex_done) error {
	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	server.mu.RLock()
	defer server.mu.RUnlock()

	var bestKey AffineKey
	var bestUse uint64

	for key, opcode := range server.opcodes {
		if opcode.UseCount > bestUse {
			bestKey = key
			bestUse = opcode.UseCount
		}
	}

	if bestUse > 0 {
		opcode := server.opcodes[bestKey]

		if setErr := res.SetKeyText(bestKey.String()); setErr != nil {
			return setErr
		}

		res.SetUseCount(opcode.UseCount)
		res.SetHardened(opcode.Hardened)
	}

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
func ComputeExpectedAffineKey(startValue, goalValue primitive.Value) AffineKey {
	return AffineKeyFromValues(startValue, goalValue)
}

/*
OpcodeForKey deterministically derives a candidate opcode from one geometric key.
This is the crystallization step: one exact gap yields one exact operator guess.
*/
func OpcodeForKey(key AffineKey) *MacroOpcode {
	delta, err := primitive.New()
	if err != nil {
		return nil
	}

	for i, block := range key {
		switch i {
		case 0:
			delta.SetC0(block)
		case 1:
			delta.SetC1(block)
		case 2:
			delta.SetC2(block)
		case 3:
			delta.SetC3(block)
		case 4:
			delta.SetC4(block)
		case 5:
			delta.SetC5(block)
		case 6:
			delta.SetC6(block)
		case 7:
			delta.SetC7(block)
		}
	}

	scale, translate := delta.RotationSeed()

	return &MacroOpcode{
		Key:       key,
		Scale:     numeric.Phase(scale),
		Translate: numeric.Phase(translate),
		UseCount:  1,
		Hardened:  false,
	}
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

	idx.opcodes[key] = OpcodeForKey(key)
}

/*
StoreOpcode inserts a pre-built MacroOpcode directly into the index.
The opcode's Key, Scale, Translate, UseCount, and Hardened fields are
stored as-is. This is used when the caller has already computed the
correct affine operator and wants to register it without going through
the RotationSeed derivation path.
*/
func (idx *MacroIndexServer) StoreOpcode(opcode *MacroOpcode) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.opcodes[opcode.Key] = opcode
}

/*
FindCandidate returns the transient synthesis record for one exact geometric gap.
*/
func (idx *MacroIndexServer) FindCandidate(key AffineKey) (*ProgramCandidate, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	candidate, exists := idx.candidates[key]
	return candidate, exists
}

/*
RecordCandidateResult updates one transient synthesis candidate with the observed
execution result. Repeated exact success promotes the candidate into a hardened
MacroOpcode without widening the key or loosening the match.
*/
func (idx *MacroIndexServer) RecordCandidateResult(
	key AffineKey,
	preResidue int,
	postResidue int,
	advanced bool,
	stable bool,
) *ProgramCandidate {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	candidate, exists := idx.candidates[key]
	if !exists {
		opcode := OpcodeForKey(key)
		candidate = &ProgramCandidate{
			Key:       key,
			Scale:     opcode.Scale,
			Translate: opcode.Translate,
		}
		idx.candidates[key] = candidate
	}

	candidate.PreResidue = preResidue
	candidate.PostResidue = postResidue
	candidate.Advanced = advanced
	candidate.Stable = stable

	success := advanced && stable && postResidue < preResidue
	if success {
		candidate.SuccessCount++

		opcode, exists := idx.opcodes[key]
		if !exists {
			opcode = &MacroOpcode{
				Key:       key,
				Scale:     candidate.Scale,
				Translate: candidate.Translate,
			}
			idx.opcodes[key] = opcode
		}

		opcode.UseCount = candidate.SuccessCount
		opcode.Hardened = candidate.SuccessCount > hardeningThreshold
	} else {
		candidate.FailureCount++
	}

	return candidate
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

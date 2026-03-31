package vm

import (
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

var (
	structureHieMu  sync.Mutex
	structureHieRNG = rand.New(rand.NewSource(42))
)

// StructureKind classifies emitted signal / cut-point results (README Signals).
type StructureKind uint8

const (
	// StructureKindLearnCancel is the learn firmware path on frame copies where the
	// decisive collapsed span is represented in the post-kernel workspace (e.g. XOR cancel map).
	StructureKindLearnCancel StructureKind = iota
	// StructureKindBuildMerge is reserved for pairwise build firmware on copies.
	StructureKindBuildMerge
)

// Structure is a cut-point emission: it references a canonical parent Value ID and
// carries a full post-kernel frame (signal workspace), without mutating the parent.
type Structure struct {
	Kind          StructureKind
	SourceValueID uint64
	ExecExit      uint16
	Frame         primitive.Value
}

// Disjoint ID range so structure frames do not collide with pooled canonical Values.
var structureFrameID uint64 = 9_000_000_000_000_000_000

func nextStructureFrameID() uint64 {
	return atomic.AddUint64(&structureFrameID, 1)
}

// StructureFromWorkspace builds a Structure from the post-UniversalBitwise workspace.
// parent must be the canonical Value (unchanged by the kernel); workspace is the mutated copy.
func StructureFromWorkspace(kind StructureKind, parent, workspace *primitive.Value) Structure {
	var s Structure
	s.Kind = kind
	if parent != nil {
		s.SourceValueID = parent[core.Cfg.Value.Region.ID.Start]
	}
	primitive.CopyFrame(&s.Frame, workspace)

	// New identity for this emission; link Prev to canonical parent for graph walks.
	s.Frame[core.Cfg.Value.Region.ID.Start] = nextStructureFrameID()
	if parent != nil {
		s.Frame[core.Cfg.Value.Region.Prev.Start] = parent[core.Cfg.Value.Region.ID.Start]

		// Holographic program recombination: blend parent and workspace genotypes in
		// spatially multiplexed HIE space so emitted structures carry continuous
		// crossover in the payload program while the firmware prefix stays intact.
		// SubstrateExploitScore (no experiment coupling) biases third-parent noise
		// toward the canonical parent when token-level signals are sharp.
		parentBias := primitive.SubstrateExploitScore(parent, workspace)
		structureHieMu.Lock()
		firmware.HolographicCrossover(
			(*[primitive.Words]uint64)(&s.Frame),
			(*[primitive.Words]uint64)(parent),
			(*[primitive.Words]uint64)(workspace),
			structureHieRNG,
			parentBias,
		)
		structureHieMu.Unlock()
	}

	hi := s.Frame[primitive.ExecStatusWord] >> primitive.ExecStatusShift
	s.ExecExit = uint16(hi)
	return s
}

func (s *Structure) lsmTokenKeys() []uint64 {
	var keys []uint64
	for _, tid := range s.Frame.TokenIDs() {
		if tid != 0 {
			keys = append(keys, tid)
		}
	}
	return keys
}

// RegisterLSM indexes this structure under derived token keys (same mechanism as NewValue).
func (s *Structure) RegisterLSM(idx *store.SpatialIndex) {
	if idx == nil {
		return
	}
	var arr [128]uint64
	copy(arr[:], s.Frame[:])
	idx.InsertBatch(s.lsmTokenKeys(), arr)
}

// RegisterDefaultLSM registers into the process-wide spatial index (with NewValue paths).
func (s *Structure) RegisterDefaultLSM() {
	s.RegisterLSM(store.DefaultSpatialIndex())
}

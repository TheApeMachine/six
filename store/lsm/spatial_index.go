package lsm

import (
	"context"
	"sync"

	"github.com/theapemachine/six/data"
)

/*
SpatialIndexServer implements the Cap'n Proto RPC interface for the Lexicon.
Collision is Compression.
faces[left][right][position] -> chord
*/
type SpatialIndexServer struct {
	mu sync.RWMutex

	faces   [256][256][]data.Chord
	count   int
	reverse map[data.Chord]uint64

	ctx context.Context
}

/*
NewSpatialIndexServer creates the core state of the LSM.
*/
func NewSpatialIndexServer(ctx context.Context) *SpatialIndexServer {
	return &SpatialIndexServer{
		reverse: make(map[data.Chord]uint64),
		ctx:     ctx,
	}
}

// --- Sync Internal Accessors ---

func (idx *SpatialIndexServer) insertSync(key uint64, value data.Chord) {
	left := byte((key >> 56) & 0xFF)
	right := byte((key >> 48) & 0xFF)
	position := uint32(key & 0xFFFFFFFFFFFF)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	edge := &idx.faces[left][right]
	if int(position) < len(*edge) && (*edge)[position].ActiveCount() > 0 {
		return
	}
	if int(position) >= len(*edge) {
		grown := make([]data.Chord, position+1)
		copy(grown, *edge)
		*edge = grown
	}

	(*edge)[position] = value
	idx.count++
	idx.reverse[value] = key
}

func (idx *SpatialIndexServer) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.count
}

func (idx *SpatialIndexServer) lookupSync(tokenID uint64) data.Chord {
	left := byte((tokenID >> 56) & 0xFF)
	right := byte((tokenID >> 48) & 0xFF)
	position := uint32(tokenID & 0xFFFFFFFFFFFF)

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if int(position) >= len(idx.faces[left][right]) {
		return data.Chord{}
	}
	return idx.faces[left][right][position]
}

func (idx *SpatialIndexServer) ReverseLookup(chord data.Chord) uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.reverse[chord]
}

// --- RPC Interface Implementations ---

func (idx *SpatialIndexServer) Insert(ctx context.Context, call SpatialIndex_insert) error {
	params := call.Args()
	reqEdge, err := params.Edge()
	if err != nil {
		return err
	}

	reqChord, err := reqEdge.Chord()
	if err != nil {
		return err
	}

	left := reqEdge.Left()
	right := reqEdge.Right()
	position := reqEdge.Position()

	val := data.Chord{
		reqChord.C0(), reqChord.C1(), reqChord.C2(), reqChord.C3(),
		reqChord.C4(), reqChord.C5(), reqChord.C6(), reqChord.C7(),
	}
	key := (uint64(left) << 56) | (uint64(right) << 48) | uint64(position)

	idx.insertSync(key, val)
	return nil
}

func (idx *SpatialIndexServer) Done(ctx context.Context, call SpatialIndex_done) error {
	return nil
}

func (idx *SpatialIndexServer) Lookup(ctx context.Context, call SpatialIndex_lookup) error {
	params := call.Args()
	left := params.Left()
	right := params.Right()
	position := params.Position()

	val := idx.lookupSync((uint64(left) << 56) | (uint64(right) << 48) | uint64(position))

	res, err := call.AllocResults()
	if err != nil {
		return err
	}
	resChord, err := res.NewChord()
	if err != nil {
		return err
	}

	resChord.SetC0(val[0])
	resChord.SetC1(val[1])
	resChord.SetC2(val[2])
	resChord.SetC3(val[3])
	resChord.SetC4(val[4])
	resChord.SetC5(val[5])
	resChord.SetC6(val[6])
	resChord.SetC7(val[7])

	return nil
}

func (idx *SpatialIndexServer) QueryTransitions(ctx context.Context, call SpatialIndex_queryTransitions) error {
	params := call.Args()
	left := params.Left()
	position := params.Position()

	idx.mu.RLock()
	var results []data.Chord
	for right := range 256 {
		edge := idx.faces[left][right]
		if int(position) < len(edge) && edge[position].ActiveCount() > 0 {
			results = append(results, edge[position])
		}
	}
	idx.mu.RUnlock()

	res, err := call.AllocResults()
	if err != nil {
		return err
	}
	list, err := res.NewChords(int32(len(results)))
	if err != nil {
		return err
	}

	for i, c := range results {
		resChord := list.At(i)
		resChord.SetC0(c[0])
		resChord.SetC1(c[1])
		resChord.SetC2(c[2])
		resChord.SetC3(c[3])
		resChord.SetC4(c[4])
		resChord.SetC5(c[5])
		resChord.SetC6(c[6])
		resChord.SetC7(c[7])
	}
	return nil
}

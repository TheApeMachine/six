package lsm

import (
	"context"

	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/pool"
)

/*
SpatialLookupFunc is the capability signature broadcast over the pool.
It is the only type that crosses the lsm→graph boundary, and it is
defined entirely in terms of packages that graph already imports.
No consumer ever needs to import lsm.
*/
type SpatialLookupFunc func(ctx context.Context, chords data.Chord_List) ([][]data.Chord, [][]data.Chord, error)

/*
SpatialIndexClient owns the live Cap'n Proto client capability and exposes
a plain-Go Lookup method. It builds itself entirely from within the lsm
package – callers outside lsm receive only the SpatialLookupFunc closure.
*/
type SpatialIndexClient struct {
	client SpatialIndex
}

/*
newSpatialIndexClient wraps a ready SpatialIndex capability.
*/
func newSpatialIndexClient(client SpatialIndex) *SpatialIndexClient {
	return &SpatialIndexClient{client: client}
}

/*
Lookup resolves a chord list against the spatial index, returning fully
materialised path slices. The Cap'n Proto future is awaited internally
so callers need no knowledge of the RPC types.
*/
func (c *SpatialIndexClient) Lookup(
	ctx context.Context,
	chords data.Chord_List,
) ([][]data.Chord, [][]data.Chord, error) {
	future, release := c.client.Lookup(ctx, func(params SpatialIndex_lookup_Params) error {
		innerList, err := params.NewChords(int32(chords.Len()))
		if err != nil {
			return console.Error(err)
		}

		for i := 0; i < chords.Len(); i++ {
			dst := innerList.At(i)
			dst.CopyFrom(chords.At(i))
		}
		return nil
	})
	defer release()

	res, err := future.Struct()
	if err != nil {
		return nil, nil, console.Error(err)
	}

	pathsList, err := res.Paths()
	if err != nil {
		return nil, nil, console.Error(err)
	}
	
	metaPathsList, err := res.MetaPaths()
	if err != nil {
		return nil, nil, console.Error(err)
	}

	out := make([][]data.Chord, pathsList.Len())
	for i := 0; i < pathsList.Len(); i++ {
		ptr, err := pathsList.At(i)
		if err != nil {
			return nil, nil, console.Error(err)
		}

		row, err := data.ChordListToSlice(data.Chord_List(ptr.List()))
		if err != nil {
			return nil, nil, console.Error(err)
		}
		out[i] = row
	}
	
	metaOut := make([][]data.Chord, metaPathsList.Len())
	for i := 0; i < metaPathsList.Len(); i++ {
		ptr, err := metaPathsList.At(i)
		if err != nil {
			return nil, nil, console.Error(err)
		}

		row, err := data.ChordListToSlice(data.Chord_List(ptr.List()))
		if err != nil {
			return nil, nil, console.Error(err)
		}
		metaOut[i] = row
	}

	return out, metaOut, nil
}

/*
SpatialInsertFunc is the capability signature for inserting into the spatial index.
Defined in terms of plain types only — no lsm types cross the package boundary.
*/
type SpatialInsertFunc func(ctx context.Context, left uint8, position uint32, chord, meta data.Chord) error

/*
Insert streams a single GraphEdge+Chord into the spatial index.
All Cap'n Proto plumbing is contained here.
*/
func (c *SpatialIndexClient) Insert(ctx context.Context, left uint8, position uint32, chord, meta data.Chord) error {
	return console.Error(c.client.Insert(ctx, func(params SpatialIndex_insert_Params) error {
		edge, err := params.NewEdge()
		if err != nil {
			return console.Error(err)
		}

		edge.SetLeft(left)
		edge.SetPosition(position)

		dst, err := edge.NewChord()
		if err != nil {
			return console.Error(err)
		}

		dst.CopyFrom(chord)
		
		metaDst, err := edge.NewMeta()
		if err != nil {
			return console.Error(err)
		}
		
		metaDst.CopyFrom(meta)
		return nil
	}))
}

/*
AsLookupFunc returns a SpatialLookupFunc closure wrapping this client.
AsInsertFunc returns a SpatialInsertFunc closure wrapping this client.
*/
func (c *SpatialIndexClient) AsLookupFunc() SpatialLookupFunc { return c.Lookup }
func (c *SpatialIndexClient) AsInsertFunc() SpatialInsertFunc { return c.Insert }

/*
SpatialLookupKey is the canonical broadcast key for the spatial lookup capability.
SpatialInsertKey is the canonical broadcast key for the spatial insert capability.
*/
const (
	SpatialLookupKey = "spatial_lookup"
	SpatialInsertKey = "spatial_insert"
)

/*
announceSpatialClient sends both SpatialLookupFunc and SpatialInsertFunc over the
broadcast group. Called by SpatialIndexServer.Announce after the RPC connection is live.
*/
func announceSpatialClient(broadcast *pool.BroadcastGroup, client SpatialIndex) {
	sc := newSpatialIndexClient(client)

	broadcast.Send(&pool.Result{
		Value: pool.PoolValue[SpatialLookupFunc]{
			Key:   SpatialLookupKey,
			Value: sc.AsLookupFunc(),
		},
	})

	broadcast.Send(&pool.Result{
		Value: pool.PoolValue[SpatialInsertFunc]{
			Key:   SpatialInsertKey,
			Value: sc.AsInsertFunc(),
		},
	})
}

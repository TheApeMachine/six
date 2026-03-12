package lsm

import (
	"context"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
)

/*
SpatialLookupFunc is the capability signature broadcast over the pool.
It is the only type that crosses the lsm→graph boundary, and it is
defined entirely in terms of packages that graph already imports.
No consumer ever needs to import lsm.
*/
type SpatialLookupFunc func(ctx context.Context, chords data.Chord_List) ([][]data.Chord, error)

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
) ([][]data.Chord, error) {
	future, release := c.client.Lookup(ctx, func(params SpatialIndex_lookup_Params) error {
		return errnie.Then(
			errnie.Try(params.NewChords(int32(chords.Len()))),
			func(innerList data.Chord_List) (data.Chord_List, error) {
				return innerList, errnie.ForEach(chords.Len(), func(i int) error {
					dst := innerList.At(i)
					dst.CopyFrom(chords.At(i))
					return nil
				})
			},
		).Err()
	})
	defer release()

	paths := make([][]data.Chord, 0)

	return paths, errnie.Then(
		errnie.Try(future.Struct()),
		func(res SpatialIndex_lookup_Results) ([][]data.Chord, error) {
			return errnie.Then(
				errnie.Try(res.Paths()),
				func(pathsList capnp.PointerList) ([][]data.Chord, error) {
					out := make([][]data.Chord, pathsList.Len())

					return out, errnie.ForEach(pathsList.Len(), func(i int) error {
						return errnie.Then(
							errnie.Try(pathsList.At(i)),
							func(ptr capnp.Ptr) ([]data.Chord, error) {
								row, err := data.ChordListToSlice(data.Chord_List(ptr.List()))
								out[i] = row
								return row, err
							},
						).Err()
					})
				},
			).Unwrap()
		},
	).Err()
}


/*
SpatialInsertFunc is the capability signature for inserting into the spatial index.
Defined in terms of plain types only — no lsm types cross the package boundary.
*/
type SpatialInsertFunc func(ctx context.Context, left uint8, position uint32, chord data.Chord) error

/*
Insert streams a single GraphEdge+Chord into the spatial index.
All Cap'n Proto plumbing is contained here.
*/
func (c *SpatialIndexClient) Insert(ctx context.Context, left uint8, position uint32, chord data.Chord) error {
	return c.client.Insert(ctx, func(params SpatialIndex_insert_Params) error {
		return errnie.Then(
			errnie.Try(params.NewEdge()),
			func(edge GraphEdge) (GraphEdge, error) {
				edge.SetLeft(left)
				edge.SetPosition(position)

				return edge, errnie.Then(
					errnie.Try(edge.NewChord()),
					func(dst data.Chord) (data.Chord, error) {
						dst.CopyFrom(chord)
						return dst, edge.SetChord(dst)
					},
				).Err()
			},
		).Err()
	})
}

/*
AsLookupFunc returns a SpatialLookupFunc closure wrapping this client.
AsInsertFunc returns a SpatialInsertFunc closure wrapping this client.
*/
func (c *SpatialIndexClient) AsLookupFunc() SpatialLookupFunc { return c.Lookup }
func (c *SpatialIndexClient) AsInsertFunc() SpatialInsertFunc  { return c.Insert }


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


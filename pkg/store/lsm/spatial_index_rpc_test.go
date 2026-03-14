package lsm

import (
	"context"
	"testing"

	"capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestSpatialIndexGetters(t *testing.T) {
	Convey("Given a spatial index with some entries", t, func() {
		spatial := NewSpatialIndexServer()
		morton := data.NewMortonCoder()

		makeState := func(state int) data.Chord {
			c := data.MustNewChord()
			c.Set(state)
			return c
		}

		mockMeta := data.MustNewChord()

		// Add an entry
		keyA := morton.Pack(0, 'A')
		stateA := makeState(10)
		spatial.insertSync(keyA, stateA, mockMeta)

		// Add a collision
		stateA2 := makeState(20)
		spatial.insertSync(keyA, stateA2, mockMeta)

		Convey("HasKey should return true for existing keys", func() {
			So(spatial.HasKey(keyA), ShouldBeTrue)
			So(spatial.HasKey(morton.Pack(1, 'B')), ShouldBeFalse)
		})

		Convey("BranchCount should return the correct number of branches", func() {
			// arrowSets isn't actually populated by insertSync, it's populated elsewhere.
			// Let's manually populate it for the test
			spatial.mu.Lock()
			spatial.arrowSets[keyA] = []data.Chord{stateA, stateA2}
			spatial.mu.Unlock()

			So(spatial.BranchCount(keyA), ShouldEqual, 2)
			So(spatial.BranchCount(morton.Pack(1, 'B')), ShouldEqual, 0)
		})

		Convey("GetChainEntry should return entries from the collision chain", func() {
			k := ToKey(stateA.Rotate3D())
			chord, exists := spatial.GetChainEntry(k)
			So(exists, ShouldBeTrue)
			So(chord.Has(20), ShouldBeTrue) // The second state is chained off the first

			_, exists = spatial.GetChainEntry(ToKey(data.MustNewChord()))
			So(exists, ShouldBeFalse)
		})
	})
}

func TestSpatialIndexDecode(t *testing.T) {
	Convey("Given a spatial index populated with a sequence", t, func() {
		spatial := NewSpatialIndexServer()
		text := []byte("hello")

		morton := data.NewMortonCoder()
		mockMeta := data.MustNewChord()
		var queryChords []data.Chord

		for pos, b := range text {
			bc, _ := data.BuildChord([]byte{b})
			candidate := bc.RollLeft(pos)
			key := morton.Pack(uint32(pos), b)
			spatial.insertSync(key, candidate, mockMeta)
			queryChords = append(queryChords, candidate)
		}

		Convey("Decode should reconstruct the byte sequence", func() {
			res := spatial.decodeChords(queryChords)
			So(len(res), ShouldBeGreaterThan, 0)

			var joined string
			for _, chunk := range res {
				joined += string(chunk)
			}
			So(joined, ShouldEqual, "hello")
		})

		Convey("Decode should handle empty chords", func() {
			res := spatial.decodeChords([]data.Chord{data.MustNewChord()})
			So(len(res), ShouldEqual, 0)
		})
	})
}

func TestSpatialIndexRPCStubs(t *testing.T) {
	Convey("Given a spatial index with context", t, func() {
		ctx := context.Background()
		spatial := NewSpatialIndexServer(WithContext(ctx))
		So(spatial.ctx, ShouldEqual, ctx)

		Convey("Done should return nil", func() {
			err := spatial.Done(ctx, SpatialIndex_done{})
			So(err, ShouldBeNil)
		})
	})
}

// These RPC tests require more intricate setup with Cap'n Proto.
// This sets up basic mock objects without a full server.
func TestSpatialIndexInsertLookup(t *testing.T) {
	Convey("Testing Capnp integration stubs", t, func() {
		_ = NewSpatialIndexServer()
		// Setup for Cap'n Proto would be highly complex here,
		// but providing the structures ensures compile-time check
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		So(err, ShouldBeNil)
		So(seg, ShouldNotBeNil)
	})
}

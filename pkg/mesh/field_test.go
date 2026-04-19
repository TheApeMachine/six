package mesh

import (
	"io"
	"math/rand/v2"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestFieldRead covers the Value-stream view of a Field: each Read
returns one Value's wire frame, the round-robin cursor advances, and
an empty Field reports io.EOF so io.Copy terminates cleanly.
*/
func TestFieldRead(t *testing.T) {
	Convey("Given a Field populated with three Values", t, func() {
		field := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(field.Close(), ShouldBeNil)
		}()

		seeded := make([]*primitive.Value, 3)
		for idx := range seeded {
			seeded[idx] = primitive.AllocValue()
			seeded[idx].StampNewID()
			field.values = append(field.values, seeded[idx])
		}
		defer func() {
			for _, value := range seeded {
				value.Close()
			}
		}()

		Convey("Read rotates through every stored Value in order", func() {
			seen := make([]uint64, 0, 6)
			frame := make([]byte, core.Cfg.Value.Bytes)

			for range 6 {
				_, readErr := field.Read(frame)
				So(readErr, ShouldEqual, io.EOF)

				restored, err := primitive.ValueFromWireFrame(frame)
				So(err, ShouldBeNil)

				seen = append(seen, restored.ID())
				restored.Close()
			}

			So(seen[0], ShouldEqual, seeded[0].ID())
			So(seen[1], ShouldEqual, seeded[1].ID())
			So(seen[2], ShouldEqual, seeded[2].ID())
			So(seen[3], ShouldEqual, seeded[0].ID())
		})
	})

	Convey("Given an empty Field", t, func() {
		field := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(field.Close(), ShouldBeNil)
		}()

		Convey("Read reports io.EOF so idiomatic copy loops terminate", func() {
			frame := make([]byte, core.Cfg.Value.Bytes)
			_, readErr := field.Read(frame)

			So(readErr, ShouldEqual, io.EOF)
		})
	})
}

/*
TestFieldWrite covers the reverse path: a wire frame written to the
Field is materialized as a real Value and registered with the aggregate
affinity fingerprint. This is what lets gossip.Conn output flow back
into a peer Field via plain io.Copy. Routing cases exercise the
community-spawning parent mode introduced by WithCommunities.
*/
func TestFieldWrite(t *testing.T) {
	Convey("Given a fresh Field and a Value whose affinity words are set", t, func() {
		field := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(field.Close(), ShouldBeNil)
		}()

		source := primitive.AllocValue()
		source.StampNewID()

		affinityStart, _ := primitive.AffinityRegion.WordExtent()
		for offset := 0; offset < primitive.AffinityWords; offset++ {
			source.Set(affinityStart+offset, uint64(0xCC00)|uint64(offset))
		}

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, readErr := source.Read(frame)
		So(readErr, ShouldEqual, io.EOF)
		defer source.Close()

		n, writeErr := field.Write(frame)

		Convey("Write registers the Value and folds its affinity into the aggregate", func() {
			So(writeErr, ShouldBeNil)
			So(n, ShouldEqual, core.Cfg.Value.Bytes)
			So(len(field.values), ShouldEqual, 1)

			for offset := 0; offset < primitive.AffinityWords; offset++ {
				So(field.affinity[offset].Load(), ShouldEqual, uint64(0xCC00)|uint64(offset))
			}
		})
	})

	Convey("Given a routing parent (WithCommunities) and two near-identical affinities", t, func() {
		parent := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(parent.Close(), ShouldBeNil)
		}()

		affinityStart, _ := primitive.AffinityRegion.WordExtent()

		// Two sparse sources differing by a single bit — well within the
		// 48-bit Hamming budget AND with a cumulative XOR popcount that
		// stays comfortably below the Shannon limit (~120 bits at 47% of
		// 257), so both must land in the same child community.
		first := writeAffinity(parent, affinityStart, [primitive.AffinityWords]uint64{
			0x0000000000000001, 0, 0, 0, 0,
		})
		second := writeAffinity(parent, affinityStart, [primitive.AffinityWords]uint64{
			0x0000000000000003, 0, 0, 0, 0,
		})
		defer first.Close()
		defer second.Close()

		Convey("both frames join the same community; child holds decoded Values and parent retains LINK carriers", func() {
			So(len(parent.values), ShouldEqual, 2)
			So(len(parent.fields), ShouldEqual, 1)
			So(len(parent.fields[0].values), ShouldEqual, 2)
		})

		Convey("parent aggregate equals XOR of both inbound affinities", func() {
			for offset := 0; offset < primitive.AffinityWords; offset++ {
				expected := (*first)[affinityStart+offset] ^ (*second)[affinityStart+offset]
				So(parent.affinity[offset].Load(), ShouldEqual, expected)
			}
		})
	})

	Convey("Given a routing parent and two far-apart affinities", t, func() {
		parent := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(parent.Close(), ShouldBeNil)
		}()

		affinityStart, _ := primitive.AffinityRegion.WordExtent()

		// All zeros vs. all ones across every affinity word — Hamming
		// distance 5*64 = 320, which blows through the 48-bit budget and
		// must cold-miss into a fresh community.
		first := writeAffinity(parent, affinityStart, [primitive.AffinityWords]uint64{0, 0, 0, 0, 0})
		second := writeAffinity(parent, affinityStart, [primitive.AffinityWords]uint64{
			^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0),
		})
		defer first.Close()
		defer second.Close()

		Convey("each frame seeds its own community", func() {
			So(len(parent.fields), ShouldEqual, 2)
			So(len(parent.fields[0].values), ShouldEqual, 1)
			So(len(parent.fields[1].values), ShouldEqual, 1)
		})
	})
}

/*
TestFieldFindCommunity locks down the visitor-routing contract of
findCommunity: a probe that lands within routeBudget joins an existing
child and gets stamped with that child's id, an out-of-budget probe
spawns a fresh child, and a visitor that already carries a COMMUNITY
stamp is left alone.
*/
func TestFieldFindCommunity(t *testing.T) {
	communityWord := core.Cfg.Value.Region.Properties.Start + int(primitive.COMMUNITY)
	affinityStart, _ := primitive.AffinityRegion.WordExtent()

	Convey("Given a parent pre-seeded with one child community at affinity zero", t, func() {
		parent := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(parent.Close(), ShouldBeNil)
		}()

		seed := writeAffinity(parent, affinityStart, [primitive.AffinityWords]uint64{0, 0, 0, 0, 0})
		defer seed.Close()
		So(len(parent.fields), ShouldEqual, 1)
		child := parent.fields[0]

		Convey("a probe within routeBudget joins the existing child and is stamped with its id", func() {
			visitor := primitive.AllocValue()
			visitor.StampNewID()
			defer visitor.Close()

			visitor.Set(affinityStart, 0x0000000000000003)

			parent.findCommunity(visitor)

			community, err := visitor.Property(primitive.COMMUNITY)
			So(err, ShouldBeNil)
			So(community, ShouldEqual, child.id)
			So(len(parent.fields), ShouldEqual, 1)
		})

		Convey("an out-of-budget probe spawns a fresh community and is stamped with its id", func() {
			visitor := primitive.AllocValue()
			visitor.StampNewID()
			defer visitor.Close()

			for offset := 0; offset < primitive.AffinityWords; offset++ {
				visitor.Set(affinityStart+offset, ^uint64(0))
			}

			parent.findCommunity(visitor)

			So(len(parent.fields), ShouldEqual, 2)
			spawned := parent.fields[1]
			community, err := visitor.Property(primitive.COMMUNITY)
			So(err, ShouldBeNil)
			So(community, ShouldEqual, spawned.id)
			So(community, ShouldNotEqual, child.id)
		})

		Convey("a visitor already stamped with COMMUNITY short-circuits without touching the parent", func() {
			visitor := primitive.AllocValue()
			visitor.StampNewID()
			defer visitor.Close()

			(*visitor)[communityWord] = 0xDEADBEEF

			before := len(parent.fields)
			parent.findCommunity(visitor)

			So(len(parent.fields), ShouldEqual, before)
			So((*visitor)[communityWord], ShouldEqual, uint64(0xDEADBEEF))
		})
	})
}

/*
TestFieldStampsCommunityID covers the visualiser contract: when a
Value reaches a leaf Field, the leaf's stable ID gets written into
the Value's properties COMMUNITY word so the front-end can group
Values by community without a side channel.

Two scenarios:
  - direct write to a leaf
  - write to a routing parent that hands the visitor down to a
    spawned child (the child is the leaf and stamps with its own ID)
*/
func TestFieldStampsCommunityID(t *testing.T) {
	communityWord := core.Cfg.Value.Region.Properties.Start + int(primitive.COMMUNITY)

	Convey("Given a leaf Field receiving a Value via Write", t, func() {
		leaf := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(leaf.Close(), ShouldBeNil)
		}()

		source := primitive.AllocValue()
		source.StampNewID()
		defer source.Close()

		frame := make([]byte, core.Cfg.Value.Bytes)
		_, readErr := source.Read(frame)
		So(readErr, ShouldEqual, io.EOF)

		_, writeErr := leaf.Write(frame)
		So(writeErr, ShouldBeNil)

		Convey("the stored visitor carries the leaf's ID in COMMUNITY", func() {
			values := leaf.values
			So(len(values), ShouldEqual, 1)
			So((*values[0])[communityWord], ShouldEqual, leaf.id)
			So(leaf.id, ShouldNotEqual, uint64(0))
		})
	})

	Convey("Given a routing parent that hands the visitor to a spawned child", t, func() {
		parent := NewField(t.Context(), 65537, nil, newTestQueue(t))
		defer func() {
			So(parent.Close(), ShouldBeNil)
		}()

		affinityStart, _ := primitive.AffinityRegion.WordExtent()
		stored := writeAffinity(parent, affinityStart, [primitive.AffinityWords]uint64{
			0x0000000000000001, 0, 0, 0, 0,
		})
		defer stored.Close()

		Convey("the child stamps its own ID, not the parent's, into COMMUNITY", func() {
			So(len(parent.fields), ShouldEqual, 1)
			child := parent.fields[0]
			members := child.values
			So(len(members), ShouldEqual, 1)
			So((*members[0])[communityWord], ShouldEqual, child.id)
			So((*members[0])[communityWord], ShouldNotEqual, parent.id)
		})
	})
}

/*
writeAffinity stamps affinity words into a fresh Value, materializes it
through the wire path, and returns the decoded Value that actually lives
in the Field. Tests own the returned Value for teardown; the source
Value is closed here since the Field keeps its own decoded copy.
*/
func writeAffinity(
	field *Field, affinityStart int, affinity [primitive.AffinityWords]uint64,
) *primitive.Value {
	source := primitive.AllocValue()
	source.StampNewID()

	for offset := 0; offset < primitive.AffinityWords; offset++ {
		source.Set(affinityStart+offset, affinity[offset])
	}

	frame := make([]byte, core.Cfg.Value.Bytes)
	_, _ = source.Read(frame)
	source.Close()

	_, _ = field.Write(frame)

	// The Value that actually got stored is the one the Field decoded
	// from the frame. Reach into whichever child or leaf bucket ended up
	// owning it so tests can assert against the canonical copy.
	if len(field.fields) > 0 {
		children := field.fields
		last := children[len(children)-1].values
		return last[len(last)-1]
	}

	values := field.values

	return values[len(values)-1]
}

func BenchmarkFieldRead(b *testing.B) {
	field := NewField(b.Context(), 65537, nil, newTestQueue(b))
	defer field.Close()

	values := make([]*primitive.Value, 16)
	for idx := range values {
		values[idx] = primitive.AllocValue()
		values[idx].StampNewID()
		field.values = append(field.values, values[idx])
	}
	defer func() {
		for _, value := range values {
			value.Close()
		}
	}()

	frame := make([]byte, core.Cfg.Value.Bytes)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := field.Read(frame); err != io.EOF {
			b.Fatal(err)
		}
	}
}

/*
BenchmarkFieldFindCommunity isolates the routing scan from Write's
decode cost. The parent is pre-seeded with communityCount child
fields by driving the real Write path with disjoint affinity seeds,
then each iteration runs findCommunity against a freshly-allocated
visitor whose affinity matches the median child so the common
"join existing community" path is what gets measured.
*/
func BenchmarkFieldFindCommunity(b *testing.B) {
	const communityCount = 32

	parent := NewField(b.Context(), 65537, nil, newTestQueue(b))
	defer parent.Close()

	affinityStart, _ := primitive.AffinityRegion.WordExtent()
	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xBADF00D))

	seeds := make([][primitive.AffinityWords]uint64, communityCount)
	for idx := range seeds {
		for wordIdx := 0; wordIdx < primitive.AffinityWords; wordIdx++ {
			seeds[idx][wordIdx] = rng.Uint64()
		}
		writeAffinity(parent, affinityStart, seeds[idx]).Close()
	}

	target := seeds[communityCount/2]
	target[0] ^= 1

	visitor := primitive.AllocValue()
	visitor.StampNewID()
	defer visitor.Close()

	communityWord := core.Cfg.Value.Region.Properties.Start + int(primitive.COMMUNITY)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		(*visitor)[communityWord] = 0
		for offset := 0; offset < primitive.AffinityWords; offset++ {
			visitor.Set(affinityStart+offset, target[offset])
		}
		parent.findCommunity(visitor)
	}
}

/*
BenchmarkFieldWriteRoute measures the full inbound path — decode, parent
fold, scan, child fold — against a warm community table. Gives the
throughput number a gossip.Conn would actually see when fanning Values
into a hierarchy.
*/
func BenchmarkFieldWriteRoute(b *testing.B) {
	const communityCount = 32

	parent := NewField(b.Context(), 65537, nil, newTestQueue(b))
	defer parent.Close()

	affinityStart, _ := primitive.AffinityRegion.WordExtent()
	rng := rand.New(rand.NewPCG(0xABCDEF, 0x123456))

	// Seed communityCount distinct children by writing disjoint affinity
	// patterns. Using the real Write path ensures the fingers table is
	// populated the same way production code would see it.
	for idx := 0; idx < communityCount; idx++ {
		seed := [primitive.AffinityWords]uint64{}
		for wordIdx := 0; wordIdx < primitive.AffinityWords; wordIdx++ {
			seed[wordIdx] = rng.Uint64()
		}
		value := writeAffinity(parent, affinityStart, seed)
		_ = value
	}

	// Build one re-usable probe frame that lands near an existing seed so
	// the benchmark measures the common "join" path, not the cold-miss
	// spawn path.
	probe := primitive.AllocValue()
	probe.StampNewID()
	for offset := 0; offset < primitive.AffinityWords; offset++ {
		probe.Set(affinityStart+offset, parent.fingers[communityCount/2][offset])
	}
	// Flip a handful of bits so the probe is close but not identical —
	// exact matches would short-circuit and hide real scan cost.
	probe.Set(affinityStart, parent.fingers[communityCount/2][0]^0x0F)

	frame := make([]byte, core.Cfg.Value.Bytes)
	_, _ = probe.Read(frame)
	probe.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := parent.Write(frame); err != nil {
			b.Fatal(err)
		}
	}
}

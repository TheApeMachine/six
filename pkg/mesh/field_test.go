package mesh

import (
	"context"
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
		field := NewField(context.Background(), 65537)
		defer func() {
			So(field.Close(), ShouldBeNil)
		}()

		seeded := make([]*primitive.Value, 3)
		for idx := range seeded {
			seeded[idx] = primitive.AllocValue()
			seeded[idx].StampNewID()
			field.AddValue(seeded[idx])
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
		field := NewField(context.Background(), 65537)
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
		field := NewField(context.Background(), 65537)
		defer func() {
			So(field.Close(), ShouldBeNil)
		}()

		source := primitive.AllocValue()
		source.StampNewID()

		affinityStart, affinityWords := primitive.AffinityRegion.WordExtent()
		for offset := 0; offset < affinityWords; offset++ {
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
			So(len(field.Values()), ShouldEqual, 1)

			for offset := 0; offset < affinityWords; offset++ {
				So(field.Affinity()[offset], ShouldEqual, uint64(0xCC00)|uint64(offset))
			}
		})
	})

	Convey("Given a routing parent (WithCommunities) and two near-identical affinities", t, func() {
		parent := NewField(context.Background(), 65537, WithCommunities(8191, 48))
		defer func() {
			So(parent.Close(), ShouldBeNil)
		}()

		affinityStart, _ := primitive.AffinityRegion.WordExtent()

		// Two sources differing by a single bit — well within the default
		// 48-bit routing budget, so both must land in the same child.
		first := writeAffinity(parent, affinityStart, [affinityWords]uint64{
			0xA5A5A5A5A5A5A5A5, 0x5A5A5A5A5A5A5A5A,
			0xDEADBEEFCAFEF00D, 0xFEEDFACE12345678,
			0x0F0F0F0F0F0F0F0F,
		})
		second := writeAffinity(parent, affinityStart, [affinityWords]uint64{
			0xA5A5A5A5A5A5A5A4, 0x5A5A5A5A5A5A5A5A,
			0xDEADBEEFCAFEF00D, 0xFEEDFACE12345678,
			0x0F0F0F0F0F0F0F0F,
		})
		defer first.Close()
		defer second.Close()

		Convey("both frames join the same community and the parent holds no direct values", func() {
			So(len(parent.Values()), ShouldEqual, 0)
			So(len(parent.Fields()), ShouldEqual, 1)
			So(len(parent.Fields()[0].Values()), ShouldEqual, 2)
		})

		Convey("parent aggregate equals XOR of both inbound affinities", func() {
			for offset := 0; offset < affinityWords; offset++ {
				expected := (*first)[affinityStart+offset] ^ (*second)[affinityStart+offset]
				So(parent.Affinity()[offset], ShouldEqual, expected)
			}
		})
	})

	Convey("Given a routing parent and two far-apart affinities", t, func() {
		parent := NewField(context.Background(), 65537, WithCommunities(8191, 48))
		defer func() {
			So(parent.Close(), ShouldBeNil)
		}()

		affinityStart, _ := primitive.AffinityRegion.WordExtent()

		// All zeros vs. all ones across every affinity word — Hamming
		// distance 5*64 = 320, which blows through the 48-bit budget and
		// must cold-miss into a fresh community.
		first := writeAffinity(parent, affinityStart, [affinityWords]uint64{0, 0, 0, 0, 0})
		second := writeAffinity(parent, affinityStart, [affinityWords]uint64{
			^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0),
		})
		defer first.Close()
		defer second.Close()

		Convey("each frame seeds its own community", func() {
			So(len(parent.Fields()), ShouldEqual, 2)
			So(len(parent.Fields()[0].Values()), ShouldEqual, 1)
			So(len(parent.Fields()[1].Values()), ShouldEqual, 1)
		})
	})
}

/*
TestFieldFindCommunity locks down the argmin/budget semantics of the
unrolled scan kernel without round-tripping through wire frames, so a
regression in the hot path is visible as soon as the test file loads.
*/
func TestFieldFindCommunity(t *testing.T) {
	Convey("Given a parent with three pre-seeded community fingerprints", t, func() {
		parent := NewField(context.Background(), 65537, WithCommunities(8191, 48))
		defer func() {
			So(parent.Close(), ShouldBeNil)
		}()

		parent.fields = []*Field{{}, {}, {}}
		parent.fingers = [][affinityWords]uint64{
			{0, 0, 0, 0, 0},
			{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)},
			{0xAAAAAAAAAAAAAAAA, 0, 0, 0, 0},
		}

		Convey("an exact match short-circuits to its index", func() {
			idx := parent.findCommunity(^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0))
			So(idx, ShouldEqual, 1)
		})

		Convey("a near-match wins argmin over farther candidates", func() {
			idx := parent.findCommunity(1, 0, 0, 0, 0)
			So(idx, ShouldEqual, 0)
		})

		Convey("an out-of-budget probe reports -1 so the caller spawns a new community", func() {
			parent.routeBudget = 4

			idx := parent.findCommunity(0x5555555555555555, 0, 0, 0, 0)
			So(idx, ShouldEqual, -1)
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
	field *Field, affinityStart int, affinity [affinityWords]uint64,
) *primitive.Value {
	source := primitive.AllocValue()
	source.StampNewID()

	for offset := 0; offset < affinityWords; offset++ {
		source.Set(affinityStart+offset, affinity[offset])
	}

	frame := make([]byte, core.Cfg.Value.Bytes)
	_, _ = source.Read(frame)
	source.Close()

	_, _ = field.Write(frame)

	// The Value that actually got stored is the one the Field decoded
	// from the frame. Reach into whichever child or leaf bucket ended up
	// owning it so tests can assert against the canonical copy.
	if len(field.Fields()) > 0 {
		children := field.Fields()
		last := children[len(children)-1].Values()
		return last[len(last)-1]
	}

	values := field.Values()

	return values[len(values)-1]
}

func BenchmarkFieldRead(b *testing.B) {
	field := NewField(context.Background(), 65537)
	defer field.Close()

	values := make([]*primitive.Value, 16)
	for idx := range values {
		values[idx] = primitive.AllocValue()
		values[idx].StampNewID()
		field.AddValue(values[idx])
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
BenchmarkFieldFindCommunity isolates the hot scan kernel from Write's
decode cost. The parent is pre-seeded with communityCount fingerprints
drawn from a fixed PRNG so every iteration walks the same table and
the POPCNT pipeline stays warm. This is the number to watch when
tuning the inner loop.
*/
func BenchmarkFieldFindCommunity(b *testing.B) {
	const communityCount = 32

	parent := NewField(context.Background(), 65537, WithCommunities(8191, 48))
	defer parent.Close()

	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xBADF00D))
	parent.fingers = make([][affinityWords]uint64, communityCount)
	parent.fields = make([]*Field, communityCount)

	for idx := range parent.fingers {
		parent.fields[idx] = &Field{}
		for wordIdx := 0; wordIdx < affinityWords; wordIdx++ {
			parent.fingers[idx][wordIdx] = rng.Uint64()
		}
	}

	// Probe near the median child so the prune-on-better path fires a
	// realistic number of times instead of a degenerate best or worst case.
	target := parent.fingers[communityCount/2]
	target[0] ^= 1

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = parent.findCommunity(target[0], target[1], target[2], target[3], target[4])
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

	parent := NewField(context.Background(), 65537, WithCommunities(8191, 48))
	defer parent.Close()

	affinityStart, _ := primitive.AffinityRegion.WordExtent()
	rng := rand.New(rand.NewPCG(0xABCDEF, 0x123456))

	// Seed communityCount distinct children by writing disjoint affinity
	// patterns. Using the real Write path ensures the fingers table is
	// populated the same way production code would see it.
	for idx := 0; idx < communityCount; idx++ {
		seed := [affinityWords]uint64{}
		for wordIdx := 0; wordIdx < affinityWords; wordIdx++ {
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
	for offset := 0; offset < affinityWords; offset++ {
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

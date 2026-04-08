package kadabra

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestFieldDigestLookup(t *testing.T) {
	t.Parallel()

	Convey("nil Field misses lookup", t, func() {
		var field *Field

		_, ok := field.digestLookup(1)

		So(ok, ShouldBeFalse)
	})

	Convey("fresh Field has no digests", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-lookup", queue)

		So(nErr, ShouldBeNil)

		_, ok := node.Field.digestLookup(0xdeadbeef)

		So(ok, ShouldBeFalse)
	})
}

func TestFieldModeCount(t *testing.T) {
	t.Parallel()

	Convey("nil Field reports zero modes", t, func() {
		var field *Field

		So(field.ModeCount(), ShouldEqual, 0)
	})

	Convey("fresh Field has no projection", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-modes", queue)

		So(nErr, ShouldBeNil)

		So(node.Field.ModeCount(), ShouldEqual, 0)
	})
}

func TestFieldModeMembers(t *testing.T) {
	t.Parallel()

	Convey("negative modeIdx returns nil", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-members", queue)

		So(nErr, ShouldBeNil)

		So(node.Field.ModeMembers(-1), ShouldBeNil)
	})
}

func TestFieldModeEnergy(t *testing.T) {
	t.Parallel()

	Convey("out-of-range modeIdx returns 0 energy", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-energy", queue)

		So(nErr, ShouldBeNil)

		So(node.Field.ModeEnergy(99), ShouldEqual, 0)
	})
}

func TestFieldDominantModeIndex(t *testing.T) {
	t.Parallel()

	Convey("fresh Field dominant index is -1", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-dominant", queue)

		So(nErr, ShouldBeNil)

		So(node.Field.DominantModeIndex(), ShouldEqual, -1)
	})
}

func TestFieldDominantModeEnergy(t *testing.T) {
	t.Parallel()

	Convey("fresh Field dominant energy is 0", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-dom-nrg", queue)

		So(nErr, ShouldBeNil)

		So(node.Field.DominantModeEnergy(), ShouldEqual, 0)
	})
}

func TestFieldProject(t *testing.T) {
	t.Parallel()

	Convey("with fewer than two remote digests, Project returns nil", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-project", queue)

		So(nErr, ShouldBeNil)

		pred, err := node.Field.Project()

		So(pred, ShouldBeNil)
		So(err, ShouldBeNil)
	})
}

func BenchmarkFieldProjectNoDigests(b *testing.B) {
	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, err := NewNode(ctx, "kadabra-field-proj-bench", queue)

	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_, _ = node.Field.Project()
	}
}

func TestFieldAbsorb(t *testing.T) {
	t.Parallel()

	Convey("Absorb stores digest for later lookup", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-absorb", queue)

		So(nErr, ShouldBeNil)

		d := Digest{
			Origin:        0x100,
			SurprisalMean: 0.25,
			Epoch:         1,
		}

		node.Field.Absorb(d)

		got, ok := node.Field.digestLookup(0x100)

		So(ok, ShouldBeTrue)
		So(got.Origin, ShouldEqual, 0x100)
		So(got.SurprisalMean, ShouldAlmostEqual, 0.25, 1e-9)
	})
}

func TestFieldProjectWithAbsorbedMesh(t *testing.T) {
	pinKadabraMeshForTrieFanout(t)

	Convey("Project runs mode detection when two cluster digests align with tries", t, func() {
		ctx := context.Background()

		queue, qErr := pool.NewQueue(ctx)

		So(qErr, ShouldBeNil)

		defer func() {
			_ = queue.Close()
		}()

		node, nErr := NewNode(ctx, "kadabra-field-mesh", queue)

		So(nErr, ShouldBeNil)

		for idx := range 2 {
			payload := fmt.Appendf(nil, "mesh-%d", idx)

			value, vErr := primitive.NewValue(payload)

			So(vErr, ShouldBeNil)

			var aff [primitive.AffinityWords]uint64

			aff[0] = uint64(idx) + 0x4000
			aff[1] = uint64(idx*11) ^ 0x5555

			value.SetAffinityVector(aff)

			record := SequenceRecord{
				Value:     *value,
				Affinity:  value.AffinityVector(),
				Label:     fmt.Sprintf("mesh%d", idx),
				Publisher: node.ID,
				Key:       uint64(idx+1)<<32 | 0xae770000 | uint64(idx),
			}

			So(node.Store(record), ShouldBeNil)

			_ = value.Close()

			queue.Drain()
		}

		tries := node.triesSnapshot()

		So(len(tries), ShouldBeGreaterThanOrEqualTo, 2)

		origin0 := (node.ID << 32) | 1
		origin1 := (node.ID << 32) | 2

		node.Field.Absorb(Digest{
			Origin:          origin0,
			Affinity:        tries[0].Affinity.Vector(),
			SurprisalMean:   2.0,
			SurprisalGrowth: 0.15,
			SurprisalPrev:   1.9,
			ClassEntropy:    0.4,
			GrowthRate:      0.08,
			Epoch:           1,
		})

		node.Field.Absorb(Digest{
			Origin:          origin1,
			Affinity:        tries[1].Affinity.Vector(),
			SurprisalMean:   1.75,
			SurprisalGrowth: 0.05,
			SurprisalPrev:   1.7,
			ClassEntropy:    0.35,
			GrowthRate:      0.06,
			Epoch:           2,
		})

		projection, err := node.Field.Project()

		So(err, ShouldBeNil)
		So(projection, ShouldNotBeNil)
		So(projection.Signals[algo.GlobalPhase], ShouldNotBeNil)
		So(projection.Signals[algo.PhaseConcentration], ShouldNotBeNil)

		So(node.Field.ModeCount(), ShouldBeGreaterThan, 0)
		So(node.Field.NodePhase().Dominant().Amplitude, ShouldBeGreaterThan, 0)
		So(node.Field.GlobalPhase().Dominant().Amplitude, ShouldBeGreaterThan, 0)
		So(node.Field.DominantPhaseIndex(), ShouldBeGreaterThanOrEqualTo, 0)
		So(node.Field.DominantPhaseStrength(), ShouldBeGreaterThan, 0)

		if node.Field.DominantModeIndex() >= 0 {
			members := node.Field.ModeMembers(node.Field.DominantModeIndex())

			So(members, ShouldNotBeNil)
			So(node.Field.ModeEnergy(node.Field.DominantModeIndex()), ShouldBeGreaterThanOrEqualTo, 0)
		}
	})
}

func BenchmarkFieldProjectMesh(b *testing.B) {
	pinKadabraMeshForTrieFanout(b)

	ctx := context.Background()

	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = queue.Close()
	}()

	node, err := NewNode(ctx, "kadabra-field-mesh-bench", queue)

	if err != nil {
		b.Fatal(err)
	}

	for idx := range 2 {
		payload := fmt.Appendf(nil, "mesh-b-%d", idx)

		value, err := primitive.NewValue(payload)

		if err != nil {
			b.Fatal(err)
		}

		var aff [primitive.AffinityWords]uint64

		aff[0] = uint64(idx) + 0x5000
		aff[2] = uint64(idx * 13)

		value.SetAffinityVector(aff)

		record := SequenceRecord{
			Value:     *value,
			Affinity:  value.AffinityVector(),
			Label:     fmt.Sprintf("mb%d", idx),
			Publisher: node.ID,
			Key:       uint64(idx+99)<<32 | 0xbeb00000 | uint64(idx),
		}

		if err := node.Store(record); err != nil {
			_ = value.Close()

			b.Fatal(err)
		}

		_ = value.Close()

		queue.Drain()
	}

	tries := node.triesSnapshot()

	if len(tries) < 2 {
		b.Fatal("expected two tries")
	}

	origin0 := (node.ID << 32) | 1
	origin1 := (node.ID << 32) | 2

	node.Field.Absorb(Digest{
		Origin:          origin0,
		Affinity:        tries[0].Affinity.Vector(),
		SurprisalMean:   2.0,
		SurprisalGrowth: 0.1,
		ClassEntropy:    0.3,
		GrowthRate:      0.05,
		Epoch:           1,
	})

	node.Field.Absorb(Digest{
		Origin:          origin1,
		Affinity:        tries[1].Affinity.Vector(),
		SurprisalMean:   1.5,
		SurprisalGrowth: -0.05,
		ClassEntropy:    0.25,
		GrowthRate:      0.04,
		Epoch:           2,
	})

	b.ResetTimer()

	for b.Loop() {
		_, _ = node.Field.Project()
	}
}

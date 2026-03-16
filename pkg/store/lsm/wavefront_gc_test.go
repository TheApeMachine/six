package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestExecutionRegistersGarbageCollectKeepsHighValueCheckpoints(t *testing.T) {
	gc.Convey("Given a checkpoint trail with stale frustration crumbs and recent stable anchors", t, func() {
		registers := newExecutionRegisters()
		registers.trailLimit = 8
		meta := data.MustNewValue()
		value := data.NeutralValue()
		value.SetStatePhase(7)

		seed := &WavefrontHead{
			phase:        7,
			alignedPhase: 7,
			queryPhase:   7,
			pos:          0,
			segment:      0,
			energy:       2,
			path:         []data.Value{value},
			metaPath:     []data.Value{meta},
			visited:      map[visitMark]bool{visitFor(1, 0): true},
		}
		carry := &WavefrontHead{
			phase:        11,
			alignedPhase: 11,
			queryPhase:   11,
			pos:          2,
			segment:      0,
			energy:       6,
			path:         []data.Value{value, value},
			metaPath:     []data.Value{meta, meta},
			visited:      map[visitMark]bool{visitFor(1, 0): true, visitFor(2, 0): true},
		}
		backtrack := &WavefrontHead{
			phase:        13,
			alignedPhase: 13,
			queryPhase:   13,
			pos:          1,
			segment:      1,
			energy:       9,
			path:         []data.Value{value, value},
			metaPath:     []data.Value{meta, meta},
			visited:      map[visitMark]bool{visitFor(1, 1): true},
			stalls:       1,
		}
		anchor := &WavefrontHead{
			phase:        17,
			alignedPhase: 17,
			queryPhase:   17,
			pos:          3,
			segment:      1,
			energy:       11,
			path:         []data.Value{value, value, value, value},
			metaPath:     []data.Value{meta, meta, meta, meta},
			visited:      map[visitMark]bool{visitFor(3, 1): true},
		}
		stable := &WavefrontHead{
			phase:        19,
			alignedPhase: 19,
			queryPhase:   19,
			pos:          4,
			segment:      1,
			energy:       12,
			path:         []data.Value{value, value, value, value, value},
			metaPath:     []data.Value{meta, meta, meta, meta, meta},
			visited:      map[visitMark]bool{visitFor(4, 1): true},
		}
		current := &WavefrontHead{
			phase:        23,
			alignedPhase: 23,
			queryPhase:   23,
			pos:          5,
			segment:      1,
			energy:       20,
			path:         []data.Value{value, value, value, value, value, value},
			metaPath:     []data.Value{meta, meta, meta, meta, meta, meta},
			visited:      map[visitMark]bool{visitFor(5, 1): true},
		}

		registers.RecordCheckpoint(seed, checkpointReasonFrustration)
		registers.RecordCheckpoint(carry, checkpointReasonCarry)
		registers.RecordCheckpoint(backtrack, checkpointReasonBacktrack)
		registers.RecordCheckpoint(anchor, checkpointReasonAnchor)
		registers.RecordCheckpoint(stable, checkpointReasonStable)

		registers.GarbageCollect(current, 3, 2)

		gc.So(len(registers.trail), gc.ShouldEqual, 3)
		gc.So(registers.HasCheckpointTag(0, 0), gc.ShouldBeFalse)
		gc.So(registers.HasCheckpointTag(1, 1), gc.ShouldBeFalse)
		gc.So(registers.HasCheckpointTag(0, 2), gc.ShouldBeTrue)
		gc.So(registers.HasCheckpointTag(1, 3), gc.ShouldBeTrue)
		gc.So(registers.HasCheckpointTag(1, 4), gc.ShouldBeTrue)

		for _, checkpoint := range registers.trail {
			gc.So(checkpoint.reason, gc.ShouldNotEqual, checkpointReasonFrustration)
			gc.So(checkpoint.reason, gc.ShouldNotEqual, checkpointReasonBacktrack)
		}
	})
}

func TestWavefrontPruneDropsExpiredAndDominatedHeads(t *testing.T) {
	gc.Convey("Given multiple challenger heads crowding the same frontier", t, func() {
		wf := NewWavefront(
			NewSpatialIndexServer(),
			WavefrontWithMaxHeads(8),
			WavefrontWithBranchHygiene(2, 2, 4, 8),
		)
		meta := data.MustNewValue()
		value := data.NeutralValue()
		value.SetStatePhase(numeric.Phase(7))

		makeHead := func(phase numeric.Phase, pos uint32, energy int, stalls, frustration uint8) *WavefrontHead {
			registers := newExecutionRegisters()
			for i := uint8(0); i < stalls; i++ {
				registers.ObserveTransition(phase, phase, numeric.Phase((uint32(phase)+uint32(i)+1)%numeric.FermatPrime))
			}
			return &WavefrontHead{
				phase:        phase,
				alignedPhase: phase,
				queryPhase:   phase,
				pos:          pos,
				segment:      0,
				promptIdx:    2,
				energy:       energy,
				path:         []data.Value{value, value, value},
				metaPath:     []data.Value{meta, meta, meta},
				visited:      map[visitMark]bool{visitFor(uint64(phase), 0): true},
				stalls:       stalls,
				frustration:  frustration,
				registers:    registers,
			}
		}

		heads := []*WavefrontHead{
			makeHead(11, 3, 10, 0, 0),
			makeHead(13, 3, 11, 1, 0),
			makeHead(17, 3, 12, 3, 2),
			makeHead(19, 3, 14, 0, 1),
			makeHead(23, 4, 18, 0, 0),
		}

		pruned := wf.prune(heads)

		gc.So(len(pruned), gc.ShouldBeLessThanOrEqualTo, 3)

		frontierCount := 0
		hasExpired := false
		for _, head := range pruned {
			if head.pos == 3 {
				frontierCount++
			}
			if head.phase == 17 {
				hasExpired = true
			}
		}

		gc.So(frontierCount, gc.ShouldEqual, 2)
		gc.So(hasExpired, gc.ShouldBeFalse)
	})
}

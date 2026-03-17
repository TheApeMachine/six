package path

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestExecutionRegistersTrackResiduesAndCheckpoints(t *testing.T) {
	gc.Convey("Given a fresh substrate execution-register workspace", t, func() {
		registers := newExecutionRegisters()

		gc.Convey("it should track residue classes and candidate alignments per phase row", func() {
			penaltyExact := registers.ObserveTransition(13, 21, 21)
			penaltyDrift := registers.ObserveTransition(13, 21, 22)

			gc.So(RegisterHasBit(registers.residues[RegisterPhaseIndex(13)], 0), gc.ShouldBeTrue)
			gc.So(
				RegisterHasBit(registers.residues[RegisterPhaseIndex(13)], int(PhaseDistance(21, 22))),
				gc.ShouldBeTrue,
			)
			gc.So(RegisterHasBit(registers.alignments[RegisterPhaseIndex(21)], RegisterPhaseIndex(21)), gc.ShouldBeTrue)
			gc.So(RegisterHasBit(registers.alignments[RegisterPhaseIndex(21)], RegisterPhaseIndex(22)), gc.ShouldBeTrue)
			gc.So(penaltyExact, gc.ShouldBeLessThan, penaltyDrift)
		})

		gc.Convey("it should tag checkpoints in the transient checkpoint plane", func() {
			head := &WavefrontHead{
				phase:   21,
				pos:     3,
				segment: 1,
				energy:  7,
				path:    []data.Value{data.MustNewValue()},
				meta:    []data.Value{data.MustNewValue()},
			}

			registers.RecordCheckpoint(head, CheckpointStable)
			gc.So(registers.HasCheckpointTag(1, 3), gc.ShouldBeTrue)
		})
	})
}

func TestExecutionRegistersGarbageCollectKeepsStableAnchors(t *testing.T) {
	gc.Convey("Given a checkpoint trail with stale bridge crumbs and recent stable anchors", t, func() {
		registers := newExecutionRegisters()
		registers.trailLimit = 8

		meta := data.MustNewValue()
		value := data.NeutralValue()
		value.SetStatePhase(numeric.Phase(7))

		seed := &WavefrontHead{
			phase:   7,
			pos:     0,
			segment: 0,
			energy:  2,
			path:    []data.Value{value},
			meta:    []data.Value{meta},
		}
		bridge := &WavefrontHead{
			phase:   11,
			pos:     2,
			segment: 0,
			energy:  6,
			path:    []data.Value{value, value},
			meta:    []data.Value{meta, meta},
		}
		anchor := &WavefrontHead{
			phase:   17,
			pos:     3,
			segment: 1,
			energy:  11,
			path:    []data.Value{value, value, value, value},
			meta:    []data.Value{meta, meta, meta, meta},
		}
		stable := &WavefrontHead{
			phase:   19,
			pos:     4,
			segment: 1,
			energy:  12,
			path:    []data.Value{value, value, value, value, value},
			meta:    []data.Value{meta, meta, meta, meta, meta},
		}
		current := &WavefrontHead{
			phase:   23,
			pos:     5,
			segment: 1,
			energy:  20,
			path:    []data.Value{value, value, value, value, value, value},
			meta:    []data.Value{meta, meta, meta, meta, meta, meta},
		}

		registers.RecordCheckpoint(seed, CheckpointSeed)
		registers.RecordCheckpoint(bridge, CheckpointBridge)
		registers.RecordCheckpoint(anchor, CheckpointAnchor)
		registers.RecordCheckpoint(stable, CheckpointStable)

		registers.GarbageCollect(current, 3, 2)

		gc.So(len(registers.trail), gc.ShouldEqual, 3)
		gc.So(registers.HasCheckpointTag(0, 0), gc.ShouldBeFalse)
		gc.So(registers.HasCheckpointTag(0, 2), gc.ShouldBeTrue)
		gc.So(registers.HasCheckpointTag(1, 3), gc.ShouldBeTrue)
		gc.So(registers.HasCheckpointTag(1, 4), gc.ShouldBeTrue)

		for _, checkpoint := range registers.trail {
			gc.So(checkpoint.reason, gc.ShouldNotEqual, CheckpointSeed)
		}
	})
}

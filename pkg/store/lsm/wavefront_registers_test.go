package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestExecutionRegistersTrackResiduesAlignmentsAndCheckpointTags(t *testing.T) {
	gc.Convey("Given a fresh execution-register workspace", t, func() {
		registers := newExecutionRegisters()

		gc.Convey("it should remember residue classes and candidate alignments per phase row", func() {
			penaltyExact := registers.ObserveTransition(13, 21, 21)
			penaltyDrift := registers.ObserveTransition(13, 21, 22)

			gc.So(registerHasBit(registers.residues[registerPhaseIndex(13)], 0), gc.ShouldBeTrue)
			gc.So(registerHasBit(registers.residues[registerPhaseIndex(13)], int(phaseDistanceMod257(21, 22))), gc.ShouldBeTrue)
			gc.So(registerHasBit(registers.alignments[registerPhaseIndex(21)], registerPhaseIndex(21)), gc.ShouldBeTrue)
			gc.So(registerHasBit(registers.alignments[registerPhaseIndex(21)], registerPhaseIndex(22)), gc.ShouldBeTrue)
			gc.So(penaltyExact, gc.ShouldBeLessThan, penaltyDrift)
		})

		gc.Convey("it should tag checkpoints in the transient checkpoint plane", func() {
			head := &WavefrontHead{
				phase:        21,
				alignedPhase: 21,
				queryPhase:   21,
				pos:          3,
				segment:      1,
				promptIdx:    2,
				energy:       7,
				path:         []data.Chord{data.MustNewChord()},
				metaPath:     []data.Chord{data.MustNewChord()},
				visited:      map[visitMark]bool{visitFor(1, 1): true},
			}

			registers.RecordCheckpoint(head, checkpointReasonStable)
			gc.So(registers.HasCheckpointTag(1, 3), gc.ShouldBeTrue)
		})
	})
}

func TestBacktrackPromptHeadRewindsToLatestCheckpoint(t *testing.T) {
	gc.Convey("Given a head that has drifted beyond a stable checkpoint", t, func() {
		wf := NewWavefront(NewSpatialIndexServer(), WavefrontWithMaxFuzzy(2))
		meta := data.MustNewChord()
		base := data.MustNewChord()
		mid := data.MustNewChord()
		cur := data.MustNewChord()

		start := &WavefrontHead{
			phase:        11,
			alignedPhase: 11,
			queryPhase:   11,
			pos:          0,
			segment:      0,
			promptIdx:    1,
			energy:       5,
			path:         []data.Chord{base},
			metaPath:     []data.Chord{meta},
			visited:      map[visitMark]bool{visitFor(1, 0): true},
		}
		wf.initializeHeadRegisters(start, checkpointReasonSeed)

		current := &WavefrontHead{
			phase:        29,
			alignedPhase: 29,
			queryPhase:   31,
			pos:          2,
			segment:      0,
			promptIdx:    3,
			energy:       17,
			path:         []data.Chord{base, mid, cur},
			metaPath:     []data.Chord{meta, meta, meta},
			visited: map[visitMark]bool{
				visitFor(1, 0): true,
				visitFor(2, 0): true,
				visitFor(3, 0): true,
			},
			fuzzyErrs: 0,
			registers: cloneExecutionRegisters(start.registers),
		}

		checkpointHead := &WavefrontHead{
			phase:        19,
			alignedPhase: 19,
			queryPhase:   19,
			pos:          1,
			segment:      0,
			promptIdx:    2,
			energy:       9,
			path:         []data.Chord{base, mid},
			metaPath:     []data.Chord{meta, meta},
			visited: map[visitMark]bool{
				visitFor(1, 0): true,
				visitFor(2, 0): true,
			},
		}
		current.registers.RecordCheckpoint(checkpointHead, checkpointReasonStable)

		rewound := wf.backtrackPromptHead(current, 'z')
		gc.So(rewound, gc.ShouldNotBeNil)
		gc.So(rewound.pos, gc.ShouldEqual, checkpointHead.pos)
		gc.So(rewound.promptIdx, gc.ShouldEqual, checkpointHead.promptIdx)
		gc.So(len(rewound.path), gc.ShouldEqual, len(checkpointHead.path))
		gc.So(rewound.fuzzyErrs, gc.ShouldEqual, 1)
		gc.So(rewound.registers.HasCheckpointTag(rewound.segment, rewound.pos), gc.ShouldBeTrue)
	})
}

func TestExpandPromptHeadAddsBacktrackCandidateWhenTraversalStalls(t *testing.T) {
	gc.Convey("Given a prompt head with a stable checkpoint but no forward candidates", t, func() {
		wf := NewWavefront(NewSpatialIndexServer(), WavefrontWithMaxFuzzy(2))
		meta := data.MustNewChord()
		value := data.NeutralValue()
		value.SetStatePhase(numeric.Phase(7))
		observable := data.SeedObservable('a', value)

		head := &WavefrontHead{
			phase:        13,
			alignedPhase: 13,
			queryPhase:   13,
			pos:          2,
			segment:      0,
			promptIdx:    2,
			energy:       11,
			path:         []data.Chord{observable, observable, observable},
			metaPath:     []data.Chord{meta, meta, meta},
			visited: map[visitMark]bool{
				visitFor(1, 0): true,
				visitFor(2, 0): true,
				visitFor(3, 0): true,
			},
			fuzzyErrs: 0,
			registers: newExecutionRegisters(),
		}

		checkpointHead := &WavefrontHead{
			phase:        7,
			alignedPhase: 7,
			queryPhase:   7,
			pos:          1,
			segment:      0,
			promptIdx:    1,
			energy:       4,
			path:         []data.Chord{observable, observable},
			metaPath:     []data.Chord{meta, meta},
			visited: map[visitMark]bool{
				visitFor(1, 0): true,
				visitFor(2, 0): true,
			},
		}
		head.registers.RecordCheckpoint(checkpointHead, checkpointReasonStable)

		next := wf.expandPromptHead(head, []byte("abc"), nil, nil)
		gc.So(len(next), gc.ShouldBeGreaterThan, 1) // skip + backtrack

		foundBacktrack := false
		for _, candidate := range next {
			if candidate.pos == checkpointHead.pos && candidate.promptIdx == checkpointHead.promptIdx {
				foundBacktrack = true
				break
			}
		}

		gc.So(foundBacktrack, gc.ShouldBeTrue)
	})
}

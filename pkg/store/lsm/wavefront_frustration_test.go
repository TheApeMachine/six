package lsm

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/goal"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func hardenRotations(ctx context.Context, rotations []numeric.Phase) *macro.MacroIndexServer {
	index := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
	for _, rotation := range rotations {
		for range 10 {
			index.RecordOpcode(rotation)
		}
	}
	return index
}

func TestFrustrateHeadReturnsMultipleControlledChallengers(t *testing.T) {
	calc := numeric.NewCalculus()
	ctx := context.Background()
	start := numeric.Phase(10)
	target := numeric.Phase(200)

	direct, err := macro.ComputeExpectedRotation(start, target)
	if err != nil {
		t.Fatal(err)
	}
	first := calc.Power(3, 5)
	intermediate := calc.Multiply(start, first)
	invIntermediate, err := calc.Inverse(intermediate)
	if err != nil {
		t.Fatal(err)
	}
	second := calc.Multiply(target, invIntermediate)

	macroIndex := hardenRotations(ctx, []numeric.Phase{direct, first, second})
	fe := goal.NewFrustrationEngineServer(
		goal.FrustrationWithContext(ctx),
		goal.WithSharedIndex(macroIndex),
	)
	defer fe.Close()

	wf := NewWavefront(
		NewSpatialIndexServer(),
		WavefrontWithFrustrationEngine(fe, target),
		WavefrontWithFrustrationForks(4, 0, 5000, 2),
	)

	value := data.NeutralValue()
	value.SetStatePhase(start)
	meta := data.MustNewValue()
	head := &WavefrontHead{
		phase:        start,
		alignedPhase: start,
		queryPhase:   start,
		pos:          1,
		segment:      0,
		promptIdx:    1,
		energy:       3,
		path:         []data.Value{value},
		metaPath:     []data.Value{meta},
		visited:      map[visitMark]bool{visitFor(1, 0): true},
		registers:    newExecutionRegisters(),
	}

	gc.Convey("Given more than one hardened bridge for the same frustration target", t, func() {
		challengers := wf.frustrateHead(head)
		gc.So(len(challengers), gc.ShouldBeGreaterThanOrEqualTo, 2)

		foundDirect := false
		foundComposed := false
		for _, candidate := range challengers {
			if candidate.phase != target {
				continue
			}
			switch candidate.pos {
			case head.pos + 1:
				foundDirect = true
			case head.pos + 2:
				foundComposed = true
			}
		}

		gc.So(foundDirect, gc.ShouldBeTrue)
		gc.So(foundComposed, gc.ShouldBeTrue)
	})
}

func TestFrustrateHeadCanForkFromEarlierCheckpoint(t *testing.T) {
	calc := numeric.NewCalculus()
	ctx := context.Background()
	checkpointPhase := numeric.Phase(19)
	currentPhase := numeric.Phase(29)
	target := numeric.Phase(101)

	invCheckpoint, err := calc.Inverse(checkpointPhase)
	if err != nil {
		t.Fatal(err)
	}
	rewindTool := calc.Multiply(target, invCheckpoint)

	macroIndex := hardenRotations(ctx, []numeric.Phase{rewindTool})
	fe := goal.NewFrustrationEngineServer(
		goal.FrustrationWithContext(ctx),
		goal.WithSharedIndex(macroIndex),
	)
	defer fe.Close()

	wf := NewWavefront(
		NewSpatialIndexServer(),
		WavefrontWithFrustrationEngine(fe, target),
		WavefrontWithFrustrationForks(4, 2, 256, 2),
	)

	meta := data.MustNewValue()
	value := data.NeutralValue()
	value.SetStatePhase(currentPhase)

	head := &WavefrontHead{
		phase:        currentPhase,
		alignedPhase: currentPhase,
		queryPhase:   currentPhase,
		pos:          3,
		segment:      0,
		promptIdx:    3,
		energy:       12,
		path:         []data.Value{value, value, value},
		metaPath:     []data.Value{meta, meta, meta},
		visited: map[visitMark]bool{
			visitFor(1, 0): true,
			visitFor(2, 0): true,
			visitFor(3, 0): true,
		},
		registers: newExecutionRegisters(),
	}

	checkpointHead := &WavefrontHead{
		phase:        checkpointPhase,
		alignedPhase: checkpointPhase,
		queryPhase:   checkpointPhase,
		pos:          0,
		segment:      0,
		promptIdx:    1,
		energy:       4,
		path:         []data.Value{value},
		metaPath:     []data.Value{meta},
		visited:      map[visitMark]bool{visitFor(1, 0): true},
	}
	head.registers.RecordCheckpoint(checkpointHead, checkpointReasonStable)

	gc.Convey("Given a rewind-only bridge source in the checkpoint trail", t, func() {
		challengers := wf.frustrateHead(head)
		gc.So(len(challengers), gc.ShouldBeGreaterThan, 0)

		foundRewound := false
		for _, candidate := range challengers {
			if candidate.phase == target && candidate.pos == checkpointHead.pos+1 && len(candidate.path) == len(checkpointHead.path)+1 {
				foundRewound = true
				break
			}
		}

		gc.So(foundRewound, gc.ShouldBeTrue)
	})
}

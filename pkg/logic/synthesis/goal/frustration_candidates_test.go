package goal

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestResolveCandidatesReturnsMultipleDeterministicBridges(t *testing.T) {
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

	index := hardenTools(ctx, []numeric.Phase{direct, first, second})
	fe := NewFrustrationEngineServer(
		FrustrationWithContext(ctx),
		WithSharedIndex(index),
	)
	defer fe.Close()

	gc.Convey("Given multiple hardened bridge paths to the same target", t, func() {
		candidates, err := fe.ResolveCandidates(start, target, 5000, 4)
		gc.So(err, gc.ShouldBeNil)
		gc.So(len(candidates), gc.ShouldBeGreaterThanOrEqualTo, 2)
		gc.So(len(candidates[0]), gc.ShouldEqual, 1)
		gc.So(len(candidates[1]), gc.ShouldEqual, 2)

		for _, path := range candidates[:2] {
			state := start
			for _, op := range path {
				state = calc.Multiply(state, op.Rotation)
			}
			gc.So(state, gc.ShouldEqual, target)
		}
	})
}

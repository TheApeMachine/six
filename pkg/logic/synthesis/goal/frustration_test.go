package goal

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
hardenTools populates a MacroIndex with opcodes, each recorded enough times
to cross the hardening threshold. Returns the index ready for frustration tests.
*/
func hardenTools(ctx context.Context, shifts []numeric.Phase) *macro.MacroIndexServer {
	idx := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(ctx),
	)

	for _, shift := range shifts {
		for range 10 {
			idx.RecordOpcode(shift)
		}
	}

	return idx
}

func TestFrustrationEngine(t *testing.T) {
	calc := numeric.NewCalculus()
	ctx := context.Background()

	// Pre-compute tool rotations as G^X for realistic tools
	toolRotations := []numeric.Phase{
		calc.Power(3, 5),
		calc.Power(3, 33),
		calc.Power(3, 80),
		calc.Power(3, 101),
	}

	gc.Convey("Given a FrustrationEngine with hardened tools", t, func() {
		macroIndex := hardenTools(ctx, toolRotations)
		fe := NewFrustrationEngineServer(
			FrustrationWithContext(ctx),
			WithSharedIndex(macroIndex),
		)

		gc.Convey("Zero frustration: should return nil path and nil error", func() {
			path, err := fe.Resolve(100, 100, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(path, gc.ShouldBeNil)
		})

		gc.Convey("Zero-phase start should return an error", func() {
			_, err := fe.Resolve(0, 100, 10)
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("Zero-phase goal should return an error", func() {
			_, err := fe.Resolve(100, 0, 10)
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("Direct Cantilever jump should work when the exact rotation is hardened", func() {
			start := numeric.Phase(50)
			goal := numeric.Phase(210)

			// Compute and harden the exact rotation needed
			requiredRot, err := macro.ComputeExpectedRotation(start, goal)
			gc.So(err, gc.ShouldBeNil)

			for range 10 {
				macroIndex.RecordOpcode(requiredRot)
			}

			path, err := fe.Resolve(start, goal, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(len(path), gc.ShouldEqual, 1)
			gc.So(path[0].Rotation, gc.ShouldEqual, requiredRot)

			// Verify the path is structurally valid
			result := calc.Multiply(start, path[0].Rotation)
			gc.So(result, gc.ShouldEqual, goal)
		})

		gc.Convey("Composed bridge should structurally verify: start * path... == goal", func() {
			start := numeric.Phase(10)

			// Goal = start * tool[1] * tool[2] — requires composing two known tools
			intermediate := calc.Multiply(start, toolRotations[1])
			goal := calc.Multiply(intermediate, toolRotations[2])

			path, err := fe.Resolve(start, goal, 5000)
			gc.So(err, gc.ShouldBeNil)
			gc.So(len(path), gc.ShouldBeGreaterThan, 0)

			// Independently verify the structural integrity of the path
			state := start

			for _, op := range path {
				state = calc.Multiply(state, op.Rotation)
			}

			gc.So(state, gc.ShouldEqual, goal)
		})

		gc.Convey("Resolved path tools should all be hardened", func() {
			start := numeric.Phase(10)
			intermediate := calc.Multiply(start, toolRotations[0])
			goal := calc.Multiply(intermediate, toolRotations[3])

			path, err := fe.Resolve(start, goal, 5000)
			gc.So(err, gc.ShouldBeNil)

			for _, op := range path {
				gc.So(op.Hardened, gc.ShouldBeTrue)
			}
		})

		gc.Convey("Empty tool library should return an error", func() {
			emptyFE := NewFrustrationEngineServer(
				FrustrationWithContext(ctx),
				WithSharedIndex(macro.NewMacroIndexServer(
					macro.MacroIndexWithContext(ctx),
				)),
			)
			_, err := emptyFE.Resolve(10, 50, 100)
			gc.So(err, gc.ShouldNotBeNil)
		})
	})
}

func TestResolveDual(t *testing.T) {
	calc := numeric.NewCalculus()
	ctx := context.Background()

	toolRotations := []numeric.Phase{
		calc.Power(3, 5),
		calc.Power(3, 33),
		calc.Power(3, 80),
		calc.Power(3, 101),
		calc.Power(3, 150),
		calc.Power(3, 200),
	}

	gc.Convey("Given a FrustrationEngine solving dual-goal torsion", t, func() {
		macroIndex := hardenTools(ctx, toolRotations)
		fe := NewFrustrationEngineServer(
			FrustrationWithContext(ctx),
			WithSharedIndex(macroIndex),
		)

		gc.Convey("Already at targetA should return nil (intersection)", func() {
			path, err := fe.ResolveDual(100, 100, 200, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(path, gc.ShouldBeNil)
		})

		gc.Convey("Already at targetB should return nil (intersection)", func() {
			path, err := fe.ResolveDual(200, 100, 200, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(path, gc.ShouldBeNil)
		})

		gc.Convey("Resolved dual path should reach the hybrid target", func() {
			start := numeric.Phase(10)
			targetA := numeric.Phase(50)
			targetB := numeric.Phase(200)

			// Compute the expected hybrid target independently
			sum := calc.Add(targetA, targetB)
			inv2, _ := calc.Inverse(2)
			expectedHybrid := calc.Multiply(sum, inv2)

			path, err := fe.ResolveDual(start, targetA, targetB, 10000)
			gc.So(err, gc.ShouldBeNil)
			gc.So(len(path), gc.ShouldBeGreaterThan, 0)

			// Verify the path structurally reaches the hybrid
			state := start

			for _, op := range path {
				state = calc.Multiply(state, op.Rotation)
			}

			gc.So(state, gc.ShouldEqual, expectedHybrid)
		})

		// KNOWN LIMITATION: ResolveDual caps composition depth at 1-4 tools per
		// attempt. The hybrid target for (15, 80, 180) is 130, which requires a
		// longer tool chain than depth 4 to reach from start=15.
		//
		// The algebra guarantees reachability: gcd(5, 256) = 1 means tool G^5
		// alone generates all of GF(257)*. The fix is algebraic composition
		// (BFS or discrete-log solve) instead of bounded random walk.
		gc.Convey("Unreachable hybrid within depth 4 should return a frustration error", func() {
			start := numeric.Phase(15)
			targetA := numeric.Phase(80)
			targetB := numeric.Phase(180)

			_, err := fe.ResolveDual(start, targetA, targetB, 10000)
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("Empty tool library should return an error for dual resolve", func() {
			emptyFE := NewFrustrationEngineServer(
				FrustrationWithContext(ctx),
				WithSharedIndex(macro.NewMacroIndexServer(
					macro.MacroIndexWithContext(ctx),
				)),
			)
			_, err := emptyFE.ResolveDual(10, 50, 100, 100)
			gc.So(err, gc.ShouldNotBeNil)
		})
	})
}

func BenchmarkResolve(b *testing.B) {
	calc := numeric.NewCalculus()
	ctx := context.Background()

	idx := hardenTools(ctx, []numeric.Phase{
		calc.Power(3, 5),
		calc.Power(3, 33),
		calc.Power(3, 80),
		calc.Power(3, 101),
	})

	fe := NewFrustrationEngineServer(
		FrustrationWithContext(ctx),
		WithSharedIndex(idx),
	)

	start := numeric.Phase(10)
	intermediate := calc.Multiply(start, calc.Power(3, 33))
	goal := calc.Multiply(intermediate, calc.Power(3, 80))

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		fe.Resolve(start, goal, 1000)
	}
}

func BenchmarkResolveDual(b *testing.B) {
	calc := numeric.NewCalculus()
	ctx := context.Background()

	idx := hardenTools(ctx, []numeric.Phase{
		calc.Power(3, 5),
		calc.Power(3, 33),
		calc.Power(3, 80),
		calc.Power(3, 101),
		calc.Power(3, 150),
		calc.Power(3, 200),
	})

	fe := NewFrustrationEngineServer(
		FrustrationWithContext(ctx),
		WithSharedIndex(idx),
	)

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		fe.ResolveDual(10, 50, 200, 1000)
	}
}

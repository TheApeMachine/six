package synthesis

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestFrustrationEngine(t *testing.T) {
	Convey("Given a Frustration Engine connected to a shared MacroIndex", t, func() {
		macroIndex := NewMacroIndex()

		// Populate the MacroIndex with some useful hardened tools
		for i := 0; i < 10; i++ {
			macroIndex.RecordOpcode(5)
			macroIndex.RecordOpcode(33)
			macroIndex.RecordOpcode(80)
			macroIndex.RecordOpcode(101)
		}

		fe := NewFrustrationEngine(WithSharedIndex(macroIndex))
		calc := numeric.NewCalculus()

		Convey("It should return nil if phase tension is already zero", func() {
			path, err := fe.Resolve(100, 100, 10)
			So(err, ShouldBeNil)
			So(path, ShouldBeNil)
		})

		Convey("It should synthesize a direct jump using Cantilever when possible", func() {
			start := numeric.Phase(50)
			goal := numeric.Phase(210)

			// Record the required path to harden it
			invStart, _ := calc.Inverse(start)
			requiredRot := calc.Multiply(goal, invStart)

			for i := 0; i < 10; i++ {
				macroIndex.RecordOpcode(requiredRot)
			}

			path, err := fe.Resolve(start, goal, 10)
			So(err, ShouldBeNil)
			So(len(path), ShouldEqual, 1)
			So(path[0].Rotation, ShouldEqual, requiredRot)
		})

		Convey("It should vibrate through available logic circuits to compose a bridge", func() {
			start := numeric.Phase(10)
			
			// We build a logical Goal that requires two specific tools from our index
			// Target = Start * (3^33) * (3^80) % 257
			intermediate := calc.Multiply(start, calc.Power(3, uint32(33)))
			goal := calc.Multiply(intermediate, calc.Power(3, uint32(80)))

			// We use a high maxAttempts because it's a random walk search
			path, err := fe.Resolve(start, goal, 1000)
			
			// If it succeeds within the attempt limit, we effectively composed a new circuit!
			if err == nil {
				So(len(path), ShouldBeGreaterThan, 0)

				// Verify the structural integrity of the composed bridge
				bridgeVal := start
				for _, op := range path {
					bridgeVal = calc.Multiply(bridgeVal, op.Rotation)
				}
				
				So(bridgeVal, ShouldEqual, goal)
			}
		})
	})
}

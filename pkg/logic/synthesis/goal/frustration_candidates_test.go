package goal

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
TestResolveCandidatesReturnsMultipleDeterministicBridges verifies that the
FrustrationEngine can resolve multiple deterministic paths to the same target
when the MacroIndex contains hardened opcodes that form bridge candidates.
It ensures that the returned paths are valid and only contain hardened opcodes.
*/
func TestResolveCandidatesReturnsMultipleDeterministicBridges(t *testing.T) {
	ctx := context.Background()

	startValue := data.BaseValue(10)
	targetValue := data.BaseValue(200)

	directKey := macro.AffineKeyFromValues(startValue, targetValue)

	midValue := data.BaseValue(80)
	firstKey := macro.AffineKeyFromValues(startValue, midValue)
	secondKey := macro.AffineKeyFromValues(midValue, targetValue)

	index := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(ctx),
	)
	for range 10 {
		index.RecordOpcode(directKey)
		index.RecordOpcode(firstKey)
		index.RecordOpcode(secondKey)
	}

	fe := NewFrustrationEngineServer(
		FrustrationWithContext(ctx),
		WithSharedIndex(index),
	)
	defer fe.Close()

	gc.Convey("Given multiple hardened bridge paths to the same target", t, func() {
		candidates, err := fe.ResolveCandidates(startValue, targetValue, 5000, 4)

		gc.Convey("It should return at least one candidate", func() {
			gc.So(err, gc.ShouldBeNil)
			gc.So(len(candidates), gc.ShouldBeGreaterThanOrEqualTo, 1)
		})

		gc.Convey("It should only contain hardened ops", func() {
			for _, path := range candidates {
				gc.So(len(path), gc.ShouldBeGreaterThan, 0)

				for _, op := range path {
					gc.So(op.Hardened, gc.ShouldBeTrue)
				}
			}
		})
	})
}

/*
BenchmarkResolveCandidates measures the performance of the FrustrationEngine's
ResolveCandidates method under a scenario with multiple bridge candidates.
*/
func BenchmarkResolveCandidates(b *testing.B) {
	ctx := context.Background()

	startValue := data.BaseValue(10)
	targetValue := data.BaseValue(200)
	midValue := data.BaseValue(80)

	directKey := macro.AffineKeyFromValues(startValue, targetValue)
	firstKey := macro.AffineKeyFromValues(startValue, midValue)
	secondKey := macro.AffineKeyFromValues(midValue, targetValue)

	index := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(ctx),
	)

	for range 10 {
		index.RecordOpcode(directKey)
		index.RecordOpcode(firstKey)
		index.RecordOpcode(secondKey)
	}

	fe := NewFrustrationEngineServer(
		FrustrationWithContext(ctx),
		WithSharedIndex(index),
	)
	defer fe.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = fe.ResolveCandidates(startValue, targetValue, 5000, 4)
	}
}

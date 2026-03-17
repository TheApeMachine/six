package goal

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/data"
)

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
		gc.So(err, gc.ShouldBeNil)
		gc.So(len(candidates), gc.ShouldBeGreaterThanOrEqualTo, 1)

		for _, path := range candidates {
			gc.So(len(path), gc.ShouldBeGreaterThan, 0)

			for _, op := range path {
				gc.So(op.Hardened, gc.ShouldBeTrue)
			}
		}
	})
}

package goal

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
hardenToolValues populates a MacroIndex with opcodes derived from Value pairs,
each recorded enough times to cross the hardening threshold.
*/
func hardenToolValues(ctx context.Context, pairs [][2]byte) *macro.MacroIndexServer {
	idx := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(ctx),
	)

	for _, pair := range pairs {
		key := macro.AffineKeyFromValues(data.BaseValue(pair[0]), data.BaseValue(pair[1]))
		for range 10 {
			idx.RecordOpcode(key)
		}
	}

	return idx
}

func TestFrustrationEngine(t *testing.T) {
	ctx := context.Background()

	toolPairs := [][2]byte{
		{5, 33},
		{33, 80},
		{80, 101},
		{101, 150},
	}

	gc.Convey("Given a FrustrationEngine with hardened tools", t, func() {
		macroIndex := hardenToolValues(ctx, toolPairs)
		fe := NewFrustrationEngineServer(
			FrustrationWithContext(ctx),
			WithSharedIndex(macroIndex),
		)

		gc.Convey("Identical values: should return nil path and nil error", func() {
			same := data.BaseValue(100)
			path, err := fe.Resolve(same, same, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(path, gc.ShouldBeNil)
		})

		gc.Convey("Empty start value should return an error", func() {
			empty := data.MustNewValue()
			real := data.BaseValue(100)
			_, err := fe.Resolve(empty, real, 10)
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("Empty goal value should return an error", func() {
			real := data.BaseValue(100)
			empty := data.MustNewValue()
			_, err := fe.Resolve(real, empty, 10)
			gc.So(err, gc.ShouldNotBeNil)
		})

		gc.Convey("Direct Cantilever jump should work when the exact key is hardened", func() {
			startValue := data.BaseValue(50)
			goalValue := data.BaseValue(210)

			requiredKey := macro.AffineKeyFromValues(startValue, goalValue)
			for range 10 {
				macroIndex.RecordOpcode(requiredKey)
			}

			path, err := fe.Resolve(startValue, goalValue, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(len(path), gc.ShouldEqual, 1)
			gc.So(path[0].Key, gc.ShouldResemble, requiredKey)
		})

		gc.Convey("Resolved path tools should all be hardened", func() {
			startValue := data.BaseValue(5)
			goalValue := data.BaseValue(80)

			requiredKey := macro.AffineKeyFromValues(startValue, goalValue)
			for range 10 {
				macroIndex.RecordOpcode(requiredKey)
			}

			path, err := fe.Resolve(startValue, goalValue, 5000)
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
			_, err := emptyFE.Resolve(data.BaseValue(10), data.BaseValue(50), 100)
			gc.So(err, gc.ShouldNotBeNil)
		})
	})
}

func TestResolveDual(t *testing.T) {
	ctx := context.Background()

	toolPairs := [][2]byte{
		{5, 33},
		{33, 80},
		{80, 101},
		{101, 150},
		{150, 200},
		{200, 250},
	}

	gc.Convey("Given a FrustrationEngine solving dual-goal torsion", t, func() {
		macroIndex := hardenToolValues(ctx, toolPairs)
		fe := NewFrustrationEngineServer(
			FrustrationWithContext(ctx),
			WithSharedIndex(macroIndex),
		)

		gc.Convey("Current == targetA should return nil", func() {
			same := data.BaseValue(100)
			other := data.BaseValue(200)
			path, err := fe.ResolveDual(same, same, other, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(path, gc.ShouldBeNil)
		})

		gc.Convey("Current == targetB should return nil", func() {
			same := data.BaseValue(200)
			other := data.BaseValue(100)
			path, err := fe.ResolveDual(same, other, same, 10)
			gc.So(err, gc.ShouldBeNil)
			gc.So(path, gc.ShouldBeNil)
		})

		gc.Convey("Empty tool library should return an error for dual resolve", func() {
			emptyFE := NewFrustrationEngineServer(
				FrustrationWithContext(ctx),
				WithSharedIndex(macro.NewMacroIndexServer(
					macro.MacroIndexWithContext(ctx),
				)),
			)
			_, err := emptyFE.ResolveDual(data.BaseValue(10), data.BaseValue(50), data.BaseValue(100), 100)
			gc.So(err, gc.ShouldNotBeNil)
		})
	})
}

func BenchmarkResolve(b *testing.B) {
	ctx := context.Background()

	idx := hardenToolValues(ctx, [][2]byte{
		{5, 33},
		{33, 80},
		{80, 101},
		{101, 150},
	})

	fe := NewFrustrationEngineServer(
		FrustrationWithContext(ctx),
		WithSharedIndex(idx),
	)

	startValue := data.BaseValue(10)
	goalValue := data.BaseValue(80)

	key := macro.AffineKeyFromValues(startValue, goalValue)
	for range 10 {
		idx.RecordOpcode(key)
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		fe.Resolve(startValue, goalValue, 1000)
	}
}

func BenchmarkResolveDual(b *testing.B) {
	ctx := context.Background()

	idx := hardenToolValues(ctx, [][2]byte{
		{5, 33},
		{33, 80},
		{80, 101},
		{101, 150},
		{150, 200},
		{200, 250},
	})

	fe := NewFrustrationEngineServer(
		FrustrationWithContext(ctx),
		WithSharedIndex(idx),
	)

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		fe.ResolveDual(data.BaseValue(10), data.BaseValue(50), data.BaseValue(200), 1000)
	}
}

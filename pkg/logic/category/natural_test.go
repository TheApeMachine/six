package category

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestNaturalTransformationMapping(t *testing.T) {
	Convey("Given a NaturalTransformation with registered mappings", t, func() {
		nat := NewNaturalTransformation()

		sourcePhase := numeric.Phase(42)
		targetPhase := numeric.Phase(1337)

		nat.RegisterMapping(sourcePhase, targetPhase)

		Convey("When looking up a registered phase", func() {
			result, exists := nat.MapObject(sourcePhase)

			Convey("It should return the mapped target phase", func() {
				So(exists, ShouldBeTrue)
				So(result, ShouldEqual, targetPhase)
			})
		})

		Convey("When looking up an unregistered phase", func() {
			_, exists := nat.MapObject(numeric.Phase(9999))

			Convey("It should report not found", func() {
				So(exists, ShouldBeFalse)
			})
		})
	})
}

func TestNaturalTransformationCoherence(t *testing.T) {
	Convey("Given two aligned Functors sharing the same source", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		source := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		targetF := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		targetG := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		defer source.Close()
		defer targetF.Close()
		defer targetG.Close()

		anchors := []string{"noun", "verb", "adjective"}
		for idx, name := range anchors {
			phase := numeric.Phase(300 + idx*41)
			source.RecordAnchor(name, phase, "text")
			targetF.RecordAnchor(name, phase, "text")
			targetG.RecordAnchor(name, phase+2, "code")
		}

		for idx := 0; idx < 6; idx++ {
			var sKey macro.AffineKey
			sKey[0] = uint64(idx*11 + 3)

			source.StoreOpcode(&macro.MacroOpcode{
				Key:       sKey,
				Scale:     numeric.Phase(idx + 10),
				Translate: numeric.Phase(idx + 20),
				UseCount:  10,
				Hardened:  true,
			})

			var tKeyF macro.AffineKey
			tKeyF[0] = uint64(idx*13 + 5)

			targetF.StoreOpcode(&macro.MacroOpcode{
				Key:       tKeyF,
				Scale:     numeric.Phase(idx + 50),
				Translate: numeric.Phase(idx + 60),
				UseCount:  10,
				Hardened:  true,
			})

			var tKeyG macro.AffineKey
			tKeyG[0] = uint64(idx*17 + 7)

			targetG.StoreOpcode(&macro.MacroOpcode{
				Key:       tKeyG,
				Scale:     numeric.Phase(idx + 80),
				Translate: numeric.Phase(idx + 90),
				UseCount:  10,
				Hardened:  true,
			})
		}

		functorF := NewFunctor(
			FunctorWithSource(source),
			FunctorWithTarget(targetF),
		)
		So(functorF.Align(), ShouldBeNil)

		functorG := NewFunctor(
			FunctorWithSource(source),
			FunctorWithTarget(targetG),
		)
		So(functorG.Align(), ShouldBeNil)

		Convey("When checking coherence with registered scale mappings", func() {
			nat := NewNaturalTransformation(
				NatWithFunctorF(functorF),
				NatWithFunctorG(functorG),
			)

			for idx := 0; idx < 6; idx++ {
				sKey := functorF.sourceKeys[idx]
				mappedF, _, _ := functorF.Map(sKey)
				mappedG, _, _ := functorG.Map(sKey)

				if mappedF != nil && mappedG != nil {
					nat.RegisterMapping(mappedF.Scale, mappedG.Scale)
				}
			}

			coherence, err := nat.CheckCoherence(6)

			Convey("It should return a coherence fraction without error", func() {
				So(err, ShouldBeNil)
				So(coherence, ShouldBeGreaterThanOrEqualTo, 0.0)
				So(coherence, ShouldBeLessThanOrEqualTo, 1.0)
			})
		})

		Convey("When no mappings are registered", func() {
			nat := NewNaturalTransformation(
				NatWithFunctorF(functorF),
				NatWithFunctorG(functorG),
			)

			_, err := nat.CheckCoherence(4)

			Convey("It should error on empty mapping", func() {
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestNaturalTransformationErrors(t *testing.T) {
	Convey("Given a NaturalTransformation without functors set", t, func() {
		nat := NewNaturalTransformation()
		nat.RegisterMapping(numeric.Phase(1), numeric.Phase(2))

		Convey("When checking coherence", func() {
			_, err := nat.CheckCoherence(1)

			Convey("It should return an error about missing functors", func() {
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func BenchmarkCheckCoherence(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
	targetF := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
	targetG := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
	defer source.Close()
	defer targetF.Close()
	defer targetG.Close()

	for idx, name := range []string{"a1", "a2", "a3"} {
		phase := numeric.Phase(500 + idx*67)
		source.RecordAnchor(name, phase, "text")
		targetF.RecordAnchor(name, phase, "text")
		targetG.RecordAnchor(name, phase+3, "code")
	}

	for idx := 0; idx < 10; idx++ {
		var sKey macro.AffineKey
		sKey[0] = uint64(idx*7 + 1)

		source.StoreOpcode(&macro.MacroOpcode{
			Key: sKey, Scale: numeric.Phase(idx + 10), UseCount: 10, Hardened: true,
		})

		var tF macro.AffineKey
		tF[0] = uint64(idx*11 + 2)

		targetF.StoreOpcode(&macro.MacroOpcode{
			Key: tF, Scale: numeric.Phase(idx + 40), UseCount: 10, Hardened: true,
		})

		var tG macro.AffineKey
		tG[0] = uint64(idx*13 + 3)

		targetG.StoreOpcode(&macro.MacroOpcode{
			Key: tG, Scale: numeric.Phase(idx + 70), UseCount: 10, Hardened: true,
		})
	}

	fF := NewFunctor(FunctorWithSource(source), FunctorWithTarget(targetF))
	_ = fF.Align()

	fG := NewFunctor(FunctorWithSource(source), FunctorWithTarget(targetG))
	_ = fG.Align()

	nat := NewNaturalTransformation(NatWithFunctorF(fF), NatWithFunctorG(fG))

	for idx := 0; idx < len(fF.sourceKeys); idx++ {
		mappedF, _, _ := fF.Map(fF.sourceKeys[idx])
		mappedG, _, _ := fG.Map(fG.sourceKeys[idx])

		if mappedF != nil && mappedG != nil {
			nat.RegisterMapping(mappedF.Scale, mappedG.Scale)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = nat.CheckCoherence(6)
	}
}

package category

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	config "github.com/theapemachine/six/pkg/system/core"
)

func TestEmbedKey(t *testing.T) {
	Convey("Given the EmbedKey projection function", t, func() {
		Convey("When embedding a non-zero AffineKey", func() {
			var key macro.AffineKey
			for blk := range key {
				key[blk] = uint64(blk*7 + 13)
			}

			dial := EmbedKey(key)

			Convey("It should produce a 512-dim non-zero PhaseDial", func() {
				So(len(dial), ShouldEqual, config.Numeric.NBasis)

				var magnitude float64
				for _, val := range dial {
					re, im := real(val), imag(val)
					magnitude += re*re + im*im
				}

				So(magnitude, ShouldBeGreaterThan, 0.5)
			})
		})

		Convey("When embedding two distinct keys", func() {
			var keyA, keyB macro.AffineKey

			for blk := range keyA {
				keyA[blk] = uint64(blk + 1)
				keyB[blk] = uint64(blk*31 + 997)
			}

			dialA := EmbedKey(keyA)
			dialB := EmbedKey(keyB)

			Convey("It should produce different embeddings", func() {
				sim := dialA.Similarity(dialB)
				So(sim, ShouldBeLessThan, 0.99)
			})
		})

		Convey("When embedding the same key twice", func() {
			var key macro.AffineKey
			key[0] = 42

			dialA := EmbedKey(key)
			dialB := EmbedKey(key)

			Convey("It should be deterministic", func() {
				sim := dialA.Similarity(dialB)
				So(sim, ShouldAlmostEqual, 1.0, 0.001)
			})
		})
	})
}

func TestFunctorAlign(t *testing.T) {
	Convey("Given two MacroIndex instances with shared anchors", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		source := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		target := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		defer source.Close()
		defer target.Close()

		anchorNames := []string{"alpha", "beta", "gamma"}
		for idx, name := range anchorNames {
			phase := numeric.Phase(100 + idx*37)
			source.RecordAnchor(name, phase, "text")
			target.RecordAnchor(name, phase+1, "text")
		}

		for idx := 0; idx < 8; idx++ {
			var key macro.AffineKey
			key[0] = uint64(idx*17 + 3)
			key[1] = uint64(idx*31 + 7)

			source.StoreOpcode(&macro.MacroOpcode{
				Key:       key,
				Scale:     numeric.Phase(idx + 10),
				Translate: numeric.Phase(idx + 20),
				UseCount:  10,
				Hardened:  true,
			})

			var tKey macro.AffineKey
			tKey[0] = uint64(idx*19 + 5)
			tKey[1] = uint64(idx*29 + 11)

			target.StoreOpcode(&macro.MacroOpcode{
				Key:       tKey,
				Scale:     numeric.Phase(idx + 50),
				Translate: numeric.Phase(idx + 60),
				UseCount:  10,
				Hardened:  true,
			})
		}

		functor := NewFunctor(
			FunctorWithSource(source),
			FunctorWithTarget(target),
		)

		Convey("When aligning the functor", func() {
			err := functor.Align()

			Convey("It should align without error", func() {
				So(err, ShouldBeNil)
				So(functor.aligned, ShouldBeTrue)
				So(functor.rotation, ShouldNotBeNil)
				So(functor.rotation.R, ShouldNotBeNil)
			})
		})
	})
}

func TestFunctorMap(t *testing.T) {
	Convey("Given an aligned Functor", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		source := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		target := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
		defer source.Close()
		defer target.Close()

		for idx, name := range []string{"anchor1", "anchor2", "anchor3"} {
			phase := numeric.Phase(200 + idx*53)
			source.RecordAnchor(name, phase, "text")
			target.RecordAnchor(name, phase, "text")
		}

		var sourceKeys []macro.AffineKey

		for idx := 0; idx < 6; idx++ {
			var sKey macro.AffineKey
			sKey[0] = uint64(idx*23 + 1)

			source.StoreOpcode(&macro.MacroOpcode{
				Key:      sKey,
				Scale:    numeric.Phase(idx + 5),
				UseCount: 10,
				Hardened: true,
			})

			sourceKeys = append(sourceKeys, sKey)

			var tKey macro.AffineKey
			tKey[0] = uint64(idx*29 + 2)

			target.StoreOpcode(&macro.MacroOpcode{
				Key:      tKey,
				Scale:    numeric.Phase(idx + 100),
				UseCount: 10,
				Hardened: true,
			})
		}

		functor := NewFunctor(
			FunctorWithSource(source),
			FunctorWithTarget(target),
		)

		alignErr := functor.Align()
		So(alignErr, ShouldBeNil)

		Convey("When mapping a source key to the target space", func() {
			opcode, distance, err := functor.Map(sourceKeys[0])

			Convey("It should find a target opcode with finite distance", func() {
				So(err, ShouldBeNil)
				So(opcode, ShouldNotBeNil)
				So(distance, ShouldBeGreaterThanOrEqualTo, 0)
				So(distance, ShouldBeLessThan, 3.0)
			})
		})

		Convey("When the functor is not aligned", func() {
			unaligned := NewFunctor(
				FunctorWithSource(source),
				FunctorWithTarget(target),
			)

			_, _, err := unaligned.Map(sourceKeys[0])

			Convey("It should return an error", func() {
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func BenchmarkEmbedKey(b *testing.B) {
	var key macro.AffineKey
	for blk := range key {
		key[blk] = uint64(blk*13 + 7)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = EmbedKey(key)
	}
}

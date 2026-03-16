package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestSkipIndex(t *testing.T) {
	gc.Convey("Given a spatial index with 200 bytes (alphabet=26)", t, func() {
		corpus := generateCorpus(200, 26, 99)
		idx, states := buildIndex(corpus)
		calc := numeric.NewCalculus()

		gc.Convey("When building a skip index", func() {
			skip := NewSkipIndex(idx)
			skip.Build()

			gc.Convey("Every Morton key in the index should have a root skip entry", func() {
				for key := range idx.entries {
					_, exists := skip.entries[key]
					gc.So(exists, gc.ShouldBeTrue)
				}
			})

			gc.Convey("Level-0 jump from position 0 should target position 1 exactly", func() {
				key0 := morton.Pack(0, corpus[0])
				targetKey, _, valid := skip.Jump(key0, SkipNext)
				gc.So(valid, gc.ShouldBeTrue)

				targetPos, targetSym := morton.Unpack(targetKey)
				gc.So(targetPos, gc.ShouldEqual, 1)
				gc.So(targetSym, gc.ShouldEqual, corpus[1])
			})

			gc.Convey("Level-2 (stride 16) from position 0 should target position 16", func() {
				key0 := morton.Pack(0, corpus[0])
				targetKey, _, valid := skip.Jump(key0, Skip16)
				gc.So(valid, gc.ShouldBeTrue)

				targetPos, targetSym := morton.Unpack(targetKey)
				gc.So(targetPos, gc.ShouldEqual, 16)
				gc.So(targetSym, gc.ShouldEqual, corpus[16])
			})

			gc.Convey("Level-3 (stride 64) from position 0 should target position 64", func() {
				key0 := morton.Pack(0, corpus[0])
				targetKey, _, valid := skip.Jump(key0, Skip64)
				gc.So(valid, gc.ShouldBeTrue)

				targetPos, targetSym := morton.Unpack(targetKey)
				gc.So(targetPos, gc.ShouldEqual, 64)
				gc.So(targetSym, gc.ShouldEqual, corpus[64])
			})

			gc.Convey("Jump to non-existent key should return invalid", func() {
				_, _, valid := skip.Jump(0xDEADBEEF, SkipNext)
				gc.So(valid, gc.ShouldBeFalse)
			})

			gc.Convey("SkipSearch path values should match actual stored state values", func() {
				startKey := morton.Pack(0, corpus[0])
				startPhase := calc.Multiply(
					numeric.Phase(1),
					calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(corpus[0])),
				)

				path := skip.SkipSearch(startKey, startPhase)

				for _, value := range path {
					found := false

					for _, s := range states {
						if value.Has(int(s)) {
							found = true
							break
						}
					}

					gc.So(found, gc.ShouldBeTrue)
				}
			})

			gc.Convey("Validate should confirm all level-0 jumps are structurally consistent", func() {
				validated := 0
				total := 0

				for i := 0; i < len(corpus)-1; i++ {
					key := morton.Pack(uint32(i), corpus[i])
					_, _, valid := skip.Jump(key, SkipNext)

					if valid {
						total++

						if skip.Validate(key, SkipNext) {
							validated++
						}
					}
				}

				gc.So(total, gc.ShouldBeGreaterThan, 0)
				gc.So(validated, gc.ShouldEqual, total)
			})
		})
	})
}

func TestSkipIndexFollowsResetAwareCompressedTraversal(t *testing.T) {
	gc.Convey("Given a compressed reset path that reuses the same radix cell", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		aPhase := calc.Multiply(1, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))
		abPhase := calc.Multiply(aPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('b')))
		abaPhase := calc.Multiply(abPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))

		keyA := morton.Pack(0, 'a')
		keyB := morton.Pack(1, 'b')

		idx.insertSync(keyA, observableValue('a', aPhase, data.OpcodeNext, 'b'), data.MustNewValue())
		idx.insertSync(keyB, observableValue('b', abPhase, data.OpcodeReset, 'a'), data.MustNewValue())
		idx.insertSync(keyA, observableValue('a', abaPhase, data.OpcodeHalt, 0), data.MustNewValue())

		skip := NewSkipIndex(idx)
		skip.Build()

		gc.Convey("A level-0 jump from the reset node should land back on local depth 0", func() {
			targetKey, targetPhase, valid := skip.Jump(keyB, SkipNext)
			gc.So(valid, gc.ShouldBeTrue)
			gc.So(targetKey, gc.ShouldEqual, keyA)
			gc.So(targetPhase, gc.ShouldEqual, abaPhase)

			entry := skip.entries[keyB]
			gc.So(entry.Levels[SkipNext].SegmentDelta, gc.ShouldEqual, 1)
		})

		gc.Convey("SkipSearch should traverse through reset and decode the repeated cell", func() {
			path := skip.SkipSearch(keyA, aPhase)
			gc.So(len(path), gc.ShouldEqual, 3)

			decoded := idx.decodeValues(path)
			gc.So(len(decoded), gc.ShouldBeGreaterThan, 0)
			gc.So(string(decoded[0]), gc.ShouldEqual, "aba")
		})
	})
}

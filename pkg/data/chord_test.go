package data

import (
	"context"
	"fmt"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/pool"
)

/*
genChordsFromBytes produces chords from byte sequences for realistic test data.
Varied lengths and byte patterns exercise BuildChord, ChordLCM, and similarity.
*/
func genChordsFromBytes(sequences [][]byte) []Chord {
	out := make([]Chord, len(sequences))
	for idx, payload := range sequences {
		chord, err := BuildChord(payload)
		if err != nil {
			panic(err)
		}
		out[idx] = chord
	}
	return out
}

func TestSanitize(t *testing.T) {
	Convey("Given a chord with polluted high bits", t, func() {
		chord := mustNewChord()
		chord.SetC4(0xFFFFFFFFFFFFFFFF)
		chord.SetC5(0xFFFFFFFFFFFFFFFF)
		chord.SetC6(0xFFFFFFFFFFFFFFFF)
		chord.SetC7(0xFFFFFFFFFFFFFFFF)

		Convey("When Sanitize is called", func() {
			chord.Sanitize()

			Convey("It should zero bits above 256 except delimiter face", func() {
				So(chord.C4(), ShouldEqual, uint64(1))
				So(chord.C5(), ShouldEqual, uint64(0))
				So(chord.C6(), ShouldEqual, uint64(0))
				So(chord.C7(), ShouldEqual, uint64(0))
			})
		})
	})

	Convey("Given a chord with low bits set", t, func() {
		chord := mustNewChord()
		chord.SetC0(0xDEADBEEF)
		chord.SetC1(0xCAFEBABE)
		chord.SetC2(0x12345678)
		chord.SetC3(0xABCDEF01)
		chord.SetC4(0x03)

		Convey("When Sanitize is called", func() {
			chord.Sanitize()

			Convey("It should preserve low bits and only keep bit 256 in C4", func() {
				So(chord.C0(), ShouldEqual, uint64(0xDEADBEEF))
				So(chord.C1(), ShouldEqual, uint64(0xCAFEBABE))
				So(chord.C2(), ShouldEqual, uint64(0x12345678))
				So(chord.C3(), ShouldEqual, uint64(0xABCDEF01))
				So(chord.C4(), ShouldEqual, uint64(1))
			})
		})
	})
}

func TestChordOR(t *testing.T) {
	Convey("Given two chords with dirty high bits", t, func() {
		a := mustNewChord()
		b := mustNewChord()
		a.SetC0(0xFF)
		a.SetC5(0x01)
		b.SetC0(0xFF00)
		b.SetC6(0x01)

		Convey("When ChordOR is called", func() {
			result := a.OR(b)

			Convey("It should OR low bits and sanitize high bits", func() {
				So(result.C0(), ShouldEqual, uint64(0xFFFF))
				So(result.C5(), ShouldEqual, uint64(0))
				So(result.C6(), ShouldEqual, uint64(0))
			})
		})
	})
}

func TestBaseChord(t *testing.T) {
	logicalBits := config.Numeric.VocabSize + 1

	Convey("Given BaseChord for each byte 0-255", t, func() {
		Convey("It should keep all bits within logical width", func() {
			for byteVal := range config.Numeric.VocabSize {
				chord := BaseChord(byte(byteVal))

				for idx := logicalBits; idx < 512; idx++ {
					word := idx / 64
					bit := idx % 64
					So(chord.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
				}

				So(chord.ActiveCount(), ShouldBeGreaterThan, 0)
			}
		})

		Convey("It should produce unique chords per byte", func() {
			chords := make(map[Chord]byte)
			for byteVal := range config.Numeric.VocabSize {
				chord := BaseChord(byte(byteVal))
				_, exists := chords[chord]
				So(exists, ShouldBeFalse)
				chords[chord] = byte(byteVal)
			}
		})
	})
}

func TestRollLeft(t *testing.T) {
	logicalBits := config.Numeric.VocabSize + 1

	Convey("Given a base chord and RollLeft(42)", t, func() {
		chord := BaseChord('A')
		rolled := chord.RollLeft(42)

		Convey("It should stay within logical width", func() {
			for idx := logicalBits; idx < 512; idx++ {
				word := idx / 64
				bit := idx % 64
				So(rolled.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
			}
		})

		Convey("It should preserve active count", func() {
			So(rolled.ActiveCount(), ShouldEqual, chord.ActiveCount())
		})
	})
}

func TestRotationSeed(t *testing.T) {
	Convey("Given two chords with same density but different structure", t, func() {
		left := mustNewChord()
		left.Set(3)
		left.Set(17)
		left.Set(41)

		right := mustNewChord()
		right.Set(5)
		right.Set(19)
		right.Set(43)

		Convey("It should use structure not density only for seed", func() {
			So(left.ActiveCount(), ShouldEqual, right.ActiveCount())

			aLeft, bLeft := left.RotationSeed()
			aRight, bRight := right.RotationSeed()

			So([2]uint16{aLeft, bLeft}, ShouldNotEqual, [2]uint16{aRight, bRight})
		})
	})
}

func TestMaskChord(t *testing.T) {
	Convey("Given MaskChord", t, func() {
		mask := MaskChord()

		Convey("It should use the control face", func() {
			So(mask.ActiveCount(), ShouldEqual, 1)
			So(mask.Has(config.Numeric.VocabSize), ShouldBeTrue)
		})
	})
}

func TestBuildChord(t *testing.T) {
	sequences := [][]byte{
		[]byte("a"),
		[]byte("ab"),
		[]byte("The quick brown fox jumps over the lazy dog."),
		bytesRepeat(config.Numeric.VocabSize),
	}

	Convey("Given byte sequences of varying length", t, func() {
		for _, payload := range sequences {
			chord, err := BuildChord(payload)
			So(err, ShouldBeNil)

			Convey(fmt.Sprintf("Payload len %d has non-zero active count", len(payload)), func() {
				So(chord.ActiveCount(), ShouldBeGreaterThan, 0)
			})

			Convey(fmt.Sprintf("Payload len %d stays within 257-bit width", len(payload)), func() {
				for idx := config.Numeric.VocabSize + 1; idx < 512; idx++ {
					word := idx / 64
					bit := idx % 64
					So(chord.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
				}
			})
		}
	})
}

func bytesRepeat(n int) []byte {
	const base = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 "
	out := make([]byte, n)
	for idx := range out {
		out[idx] = base[idx%len(base)]
	}
	return out
}

func TestCopyFrom(t *testing.T) {
	Convey("Given a source chord and an empty destination", t, func() {
		src := BaseChord(0xAB)
		dst := mustNewChord()

		Convey("When CopyFrom is called", func() {
			dst.CopyFrom(src)

			Convey("It should produce identical blocks", func() {
				So(dst.C0(), ShouldEqual, src.C0())
				So(dst.C1(), ShouldEqual, src.C1())
				So(dst.C2(), ShouldEqual, src.C2())
				So(dst.C3(), ShouldEqual, src.C3())
				So(dst.C4(), ShouldEqual, src.C4())
				So(dst.C5(), ShouldEqual, src.C5())
				So(dst.C6(), ShouldEqual, src.C6())
				So(dst.C7(), ShouldEqual, src.C7())
			})
		})
	})
}

func TestChordListToSlice(t *testing.T) {
	Convey("Given a Chord_List populated with chords", t, func() {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		So(err, ShouldBeNil)
		list, err := NewChord_List(seg, 4)
		So(err, ShouldBeNil)

		chords := []Chord{BaseChord('A'), BaseChord('B'), BaseChord('C'), BaseChord(0)}
		for idx, ch := range chords {
			el := list.At(idx)
			el.CopyFrom(ch)
		}

		Convey("When ChordListToSlice is called", func() {
			out, err := ChordListToSlice(list)

			Convey("It should return a slice matching the list", func() {
				So(err, ShouldBeNil)
				So(len(out), ShouldEqual, 4)
				for idx := range chords {
					So(out[idx].ActiveCount(), ShouldEqual, chords[idx].ActiveCount())
				}
			})
		})
	})
}

func TestChordLCM(t *testing.T) {
	chords := genChordsFromBytes([][]byte{[]byte("a"), []byte("ab"), []byte("abc")})

	Convey("Given a slice of chords", t, func() {
		Convey("When ChordLCM is called", func() {
			lcm := ChordLCM(chords)

			Convey("It should contain all bits from each chord", func() {
				for _, ch := range chords {
					sim := ChordSimilarity(&lcm, &ch)
					So(sim, ShouldEqual, ch.ActiveCount())
				}
			})

			Convey("It should be sanitized within 257 bits", func() {
				lcm.Sanitize()
				So(lcm.C5(), ShouldEqual, uint64(0))
				So(lcm.C6(), ShouldEqual, uint64(0))
				So(lcm.C7(), ShouldEqual, uint64(0))
			})
		})

		Convey("When ChordLCM is called on empty slice", func() {
			lcm := ChordLCM(nil)

			Convey("It should return zero chord", func() {
				So(lcm.ActiveCount(), ShouldEqual, 0)
			})
		})
	})
}

func TestHasSetClear(t *testing.T) {
	Convey("Given an empty chord", t, func() {
		chord := mustNewChord()

		Convey("When Set is called for prime indices", func() {
			for _, primeIdx := range []int{0, 1, 64, 128, 200, 256} {
				chord.Set(primeIdx)
			}

			Convey("It should report Has for each", func() {
				for _, primeIdx := range []int{0, 1, 64, 128, 200, 256} {
					So(chord.Has(primeIdx), ShouldBeTrue)
				}
			})
		})

		Convey("When Clear is called after Set", func() {
			chord.Set(42)
			chord.Clear(42)

			Convey("It should report not Has", func() {
				So(chord.Has(42), ShouldBeFalse)
			})
		})
	})
}

func TestShannonDensity(t *testing.T) {
	Convey("Given chords with known active counts", t, func() {
		empty := mustNewChord()
		base := BaseChord('A')

		Convey("ShannonDensity should reflect fraction of 257 bits", func() {
			So(empty.ShannonDensity(), ShouldEqual, 0)
			So(base.ShannonDensity(), ShouldAlmostEqual, float64(base.ActiveCount())/257.0, 0.0001)
		})
	})
}

func TestChordSimilarity(t *testing.T) {
	Convey("Given two chords with overlap", t, func() {
		left := BaseChord('a')
		right := BaseChord('a')

		Convey("Identical chords should have similarity equal to active count", func() {
			sim := ChordSimilarity(&left, &right)
			So(sim, ShouldEqual, left.ActiveCount())
		})

		Convey("Disjoint chords should have zero similarity", func() {
			disjoint := mustNewChord()
			disjoint.Set(0)
			other := mustNewChord()
			other.Set(255)

			sim := ChordSimilarity(&disjoint, &other)
			So(sim, ShouldEqual, 0)
		})
	})
}

func TestChordHole(t *testing.T) {
	Convey("Given target and existing chords", t, func() {
		target := BaseChord('x')
		existing := mustNewChord()
		existing.Set(int('x') * 7 % 257)

		Convey("When ChordHole is called", func() {
			hole := ChordHole(&target, &existing)

			Convey("It should contain bits in target but not in existing", func() {
				simWithTarget := ChordSimilarity(&hole, &target)
				simWithExisting := ChordSimilarity(&hole, &existing)
				So(simWithTarget, ShouldBeGreaterThan, 0)
				So(simWithExisting, ShouldEqual, 0)
			})
		})
	})
}

func TestChordAlgebra(t *testing.T) {
	baseA := BaseChord('A')
	baseB := BaseChord('B')

	Convey("Given chord AND/OR/XOR algebra", t, func() {
		Convey("OR of chord with itself should equal itself", func() {
			lcm := baseA.OR(baseA)
			So(ChordSimilarity(&baseA, &lcm), ShouldEqual, baseA.ActiveCount())
		})

		Convey("AND of chord with itself should equal itself", func() {
			gcd := baseA.AND(baseA)
			So(gcd.ActiveCount(), ShouldEqual, baseA.ActiveCount())
		})

		Convey("XOR of chord with itself should be zero", func() {
			xor := baseA.XOR(baseA)
			xor.Sanitize()
			So(xor.ActiveCount(), ShouldEqual, 0)
		})

		Convey("OR then AND with same chord should recover intersection", func() {
			combined := baseA.OR(baseB)
			recovered := combined.AND(baseA)
			So(ChordSimilarity(&recovered, &baseA), ShouldEqual, baseA.ActiveCount())
		})
	})
}

func TestChordBin(t *testing.T) {
	Convey("Given BaseChord for bytes 0-255", t, func() {
		bins := make(map[int]int)
		for byteVal := range config.Numeric.VocabSize {
			chord := BaseChord(byte(byteVal))
			bin := ChordBin(&chord)
			bins[bin]++
		}

		Convey("ChordBin should spread across many distinct bins", func() {
			So(len(bins), ShouldBeGreaterThan, 100)
		})

		Convey("ChordBin for same chord should be deterministic", func() {
			chord := BaseChord(42)
			b1 := ChordBin(&chord)
			b2 := ChordBin(&chord)
			So(b1, ShouldEqual, b2)
		})
	})
}

func TestFlatten(t *testing.T) {
	Convey("Given a chord", t, func() {
		chord := BaseChord('Z')

		Convey("When Flatten is called", func() {
			flat := chord.Flatten()

			Convey("FlatChord count should match ActiveCount", func() {
				So(int(flat.Count), ShouldEqual, chord.ActiveCount())
			})

			Convey("ActivePrimes indices should be within 257", func() {
				for idx := uint16(0); idx < flat.Count; idx++ {
					So(flat.ActivePrimes[idx], ShouldBeLessThan, 257)
				}
			})
		})
	})
}

func TestChordPrimeIndices(t *testing.T) {
	Convey("Given a chord", t, func() {
		chord := BaseChord(100)

		Convey("When ChordPrimeIndices is called", func() {
			indices := ChordPrimeIndices(&chord)

			Convey("It should return count matching ActiveCount", func() {
				So(len(indices), ShouldEqual, chord.ActiveCount())
			})

			Convey("Each index should be within NBasis", func() {
				for _, idx := range indices {
					So(idx, ShouldBeGreaterThanOrEqualTo, 0)
					So(idx, ShouldBeLessThan, 257)
				}
			})
		})
	})
}

func TestPopcount(t *testing.T) {
	Convey("Given Popcount", t, func() {
		Convey("It should match known values", func() {
			So(Popcount(0), ShouldEqual, 0)
			So(Popcount(1), ShouldEqual, 1)
			So(Popcount(0xFF), ShouldEqual, 8)
			So(Popcount(0xFFFFFFFFFFFFFFFF), ShouldEqual, 64)
		})
	})
}

func TestBindPosition(t *testing.T) {
	Convey("Given a chord and position", t, func() {
		chord := BaseChord('M')

		Convey("When BindPosition is called", func() {
			bound := chord.BindPosition(17)

			Convey("It should preserve active count", func() {
				So(bound.ActiveCount(), ShouldEqual, chord.ActiveCount())
			})

			Convey("It should stay within logical width", func() {
				for idx := 257; idx < 512; idx++ {
					word := idx / 64
					bit := idx % 64
					So(bound.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
				}
			})
		})
	})
}

func TestFlattenBatched(t *testing.T) {
	chords := genChordsFromBytes([][]byte{
		[]byte("first"),
		[]byte("second"),
		[]byte("third"),
		[]byte("A longer sequence to stress the chord builder"),
	})

	Convey("Given chords and a pool", t, func() {
		workerPool := pool.New(context.Background(), 2, 4, pool.NewConfig())
		defer workerPool.Close()

		Convey("When FlattenBatched is called", func() {
			flats := FlattenBatched(chords, workerPool)

			Convey("It should produce one FlatChord per input", func() {
				So(len(flats), ShouldEqual, len(chords))
				for idx := range chords {
					So(int(flats[idx].Count), ShouldEqual, chords[idx].ActiveCount())
				}
			})
		})
	})
}

func BenchmarkChordRotationSeed(b *testing.B) {
	chord := BaseChord('x')

	for b.Loop() {
		_, _ = chord.RotationSeed()
	}
}

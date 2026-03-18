package data

import (
	"context"
	"math/bits"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
genValuesFromBytes produces values from byte sequences for realistic test data.
Varied lengths and byte patterns exercise BuildValue, ValueLCM, and similarity.
*/
func genValuesFromBytes(sequences [][]byte) ([]Value, error) {
	out := make([]Value, len(sequences))
	for idx, payload := range sequences {
		value, err := BuildValue(payload)
		if err != nil {
			return nil, err
		}
		out[idx] = value
	}
	return out, nil
}

func TestSanitize(t *testing.T) {
	Convey("Given a value with polluted high bits", t, func() {
		value := MustNewValue()
		value.SetC4(0xFFFFFFFFFFFFFFFF)
		value.SetC5(0xFFFFFFFFFFFFFFFF)
		value.SetC6(0xFFFFFFFFFFFFFFFF)
		value.SetC7(0xFFFFFFFFFFFFFFFF)

		Convey("When Sanitize is called", func() {
			value.Sanitize()

			Convey("It should zero bits above 256 except delimiter face and Guard Band", func() {
				So(value.C4(), ShouldEqual, uint64(1))
				So(value.C5(), ShouldEqual, uint64(0xFFFFFFFFFFFFFFFF))
				So(value.C6(), ShouldEqual, uint64(0xFFFFFFFFFFFFFFFF))
				So(value.C7(), ShouldEqual, uint64(0xFFFFFFFFFFFFFFFF))
			})
		})
	})

	Convey("Given a value with low bits set", t, func() {
		value := MustNewValue()
		value.SetC0(0xDEADBEEF)
		value.SetC1(0xCAFEBABE)
		value.SetC2(0x12345678)
		value.SetC3(0xABCDEF01)
		value.SetC4(0x03)

		Convey("When Sanitize is called", func() {
			value.Sanitize()

			Convey("It should preserve low bits and only keep bit 256 in C4", func() {
				So(value.C0(), ShouldEqual, uint64(0xDEADBEEF))
				So(value.C1(), ShouldEqual, uint64(0xCAFEBABE))
				So(value.C2(), ShouldEqual, uint64(0x12345678))
				So(value.C3(), ShouldEqual, uint64(0xABCDEF01))
				So(value.C4(), ShouldEqual, uint64(1))
			})
		})
	})
}

func TestValueOR(t *testing.T) {
	Convey("Given two values with dirty high bits", t, func() {
		a := MustNewValue()
		b := MustNewValue()
		a.SetC0(0xFF)
		a.SetC5(0x01)
		b.SetC0(0xFF00)
		b.SetC6(0x01)

		Convey("When ValueOR is called", func() {
			result := a.OR(b)

			Convey("It should OR low bits and sanitize high bits", func() {
				So(result.C0(), ShouldEqual, uint64(0xFFFF))
				So(result.C5(), ShouldEqual, uint64(0))
				So(result.C6(), ShouldEqual, uint64(0))
			})
		})
	})
}

func TestBaseValue(t *testing.T) {
	logicalBits := config.Numeric.VocabSize + 1

	Convey("Given BaseValue for each byte", t, func() {
		Convey("It should keep all bits within logical width and be unique", func() {
			values := make(map[Value]byte)
			for byteVal := range config.Numeric.VocabSize {
				value := BaseValue(byte(byteVal))

				for idx := logicalBits; idx < 512; idx++ {
					word := idx / 64
					bit := idx % 64
					So(value.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
				}

				So(value.ActiveCount(), ShouldBeGreaterThan, 0)
				_, exists := values[value]
				So(exists, ShouldBeFalse)
				values[value] = byte(byteVal)
			}
		})
	})
}

func TestRollLeft(t *testing.T) {
	logicalBits := config.Numeric.VocabSize + 1

	Convey("Given a base value and RollLeft(42)", t, func() {
		value := BaseValue('A')
		rolled := value.RollLeft(42)

		Convey("It should stay within logical width", func() {
			for idx := logicalBits; idx < 512; idx++ {
				word := idx / 64
				bit := idx % 64
				So(rolled.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
			}
		})

		Convey("It should preserve active count", func() {
			So(rolled.ActiveCount(), ShouldEqual, value.ActiveCount())
		})
	})
}

func TestRotationSeed(t *testing.T) {
	Convey("Given two values with same density but different structure", t, func() {
		left := MustNewValue()
		left.Set(3)
		left.Set(17)
		left.Set(41)

		right := MustNewValue()
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

func TestMaskValue(t *testing.T) {
	Convey("Given MaskValue", t, func() {
		mask := MaskValue()

		Convey("It should use the control face", func() {
			So(mask.ActiveCount(), ShouldEqual, 1)
			So(mask.Has(config.Numeric.VocabSize), ShouldBeTrue)
		})
	})
}

func TestBuildValue(t *testing.T) {
	sequences := [][]byte{
		[]byte("a"),
		[]byte("ab"),
		[]byte("The quick brown fox jumps over the lazy dog."),
		bytesRepeat(config.Numeric.VocabSize),
	}

	Convey("Given byte sequences of varying length", t, func() {
		for _, payload := range sequences {
			value, err := BuildValue(payload)
			So(err, ShouldBeNil)

			So(value.ActiveCount(), ShouldBeGreaterThan, 0)

			for idx := config.Numeric.VocabSize + 1; idx < 512; idx++ {
				word := idx / 64
				bit := idx % 64
				So(value.block(word)&(1<<uint(bit)), ShouldEqual, uint64(0))
			}
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
	Convey("Given a source value and an empty destination", t, func() {
		src := BaseValue(0xAB)
		dst := MustNewValue()

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

func TestValueListToSlice(t *testing.T) {
	Convey("Given a Value_List populated with values", t, func() {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		So(err, ShouldBeNil)
		list, err := NewValue_List(seg, 4)
		So(err, ShouldBeNil)

		values := []Value{BaseValue('A'), BaseValue('B'), BaseValue('C'), BaseValue(0)}
		for idx, ch := range values {
			el := list.At(idx)
			el.CopyFrom(ch)
		}

		Convey("When ValueListToSlice is called", func() {
			out, err := ValueListToSlice(list)

			Convey("It should return a slice matching the list", func() {
				So(err, ShouldBeNil)
				So(len(out), ShouldEqual, 4)
				for idx := range values {
					So(out[idx].ActiveCount(), ShouldEqual, values[idx].ActiveCount())
				}
			})
		})
	})
}

func TestValueLCM(t *testing.T) {
	values, err := genValuesFromBytes([][]byte{[]byte("a"), []byte("ab"), []byte("abc")})

	Convey("Given a slice of values", t, func() {
		So(err, ShouldBeNil)
		Convey("When LCM is called", func() {
			lcm := Value{}.LCM(values)

			Convey("It should contain all bits from each value", func() {
				for _, ch := range values {
					sim := ch.Similarity(lcm)
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

		Convey("When LCM is called on empty slice", func() {
			lcm := Value{}.LCM(nil)

			Convey("It should return zero value", func() {
				So(lcm.ActiveCount(), ShouldEqual, 0)
			})
		})
	})
}

func TestHasSetClear(t *testing.T) {
	Convey("Given an empty value", t, func() {
		value := MustNewValue()

		Convey("When Set is called for prime indices", func() {
			for _, primeIdx := range []int{0, 1, 64, 128, 200, 256} {
				value.Set(primeIdx)
			}

			Convey("It should report Has for each", func() {
				for _, primeIdx := range []int{0, 1, 64, 128, 200, 256} {
					So(value.Has(primeIdx), ShouldBeTrue)
				}
			})
		})

		Convey("When Clear is called after Set", func() {
			value.Set(42)
			value.Clear(42)

			Convey("It should report not Has", func() {
				So(value.Has(42), ShouldBeFalse)
			})
		})
	})
}

func TestShannonDensity(t *testing.T) {
	Convey("Given values with known active counts", t, func() {
		empty := MustNewValue()
		base := BaseValue('A')

		Convey("ShannonDensity should reflect fraction of 257 bits", func() {
			So(empty.ShannonDensity(), ShouldEqual, 0)
			So(base.ShannonDensity(), ShouldAlmostEqual, float64(base.ActiveCount())/257.0, 0.0001)
		})
	})
}

func TestValueSimilarity(t *testing.T) {
	Convey("Given two values with overlap", t, func() {
		left := BaseValue('a')
		right := BaseValue('a')

		Convey("Identical values should have similarity equal to active count", func() {
			sim := left.Similarity(right)
			So(sim, ShouldEqual, left.ActiveCount())
		})

		Convey("Disjoint values should have zero similarity", func() {
			disjoint := MustNewValue()
			disjoint.Set(0)
			other := MustNewValue()
			other.Set(255)

			sim := disjoint.Similarity(other)
			So(sim, ShouldEqual, 0)
		})
	})
}

func TestValueHole(t *testing.T) {
	Convey("Given target and existing values", t, func() {
		target := BaseValue('x')
		existing := MustNewValue()
		existing.Set(int('x') * 7 % 257)

		Convey("When ValueHole is called", func() {
			hole := target.Hole(existing)

			Convey("It should contain bits in target but not in existing", func() {
				simWithTarget := hole.Similarity(target)
				simWithExisting := hole.Similarity(existing)
				So(simWithTarget, ShouldBeGreaterThan, 0)
				So(simWithExisting, ShouldEqual, 0)
			})
		})
	})
}

func TestValueAlgebra(t *testing.T) {
	baseA := BaseValue('A')
	baseB := BaseValue('B')

	Convey("Given value AND/OR/XOR algebra", t, func() {
		Convey("OR of value with itself should equal itself", func() {
			lcm := baseA.OR(baseA)
			So(lcm.Similarity(baseA), ShouldEqual, baseA.ActiveCount())
		})

		Convey("AND of value with itself should equal itself", func() {
			gcd := baseA.AND(baseA)
			So(gcd.ActiveCount(), ShouldEqual, baseA.ActiveCount())
		})

		Convey("XOR of value with itself should be zero", func() {
			xor := baseA.XOR(baseA)
			xor.Sanitize()
			So(xor.ActiveCount(), ShouldEqual, 0)
		})

		Convey("OR then AND with same value should recover intersection", func() {
			combined := baseA.OR(baseB)
			recovered := combined.AND(baseA)
			So(recovered.Similarity(baseA), ShouldEqual, baseA.ActiveCount())
		})
	})
}

func TestValueBin(t *testing.T) {
	Convey("Given BaseValue for bytes 0-255", t, func() {
		bins := make(map[int]int)
		for byteVal := range config.Numeric.VocabSize {
			value := BaseValue(byte(byteVal))
			bin := value.Bin()
			bins[bin]++
		}

		Convey("ValueBin should spread across many distinct bins", func() {
			So(len(bins), ShouldBeGreaterThan, 100)
		})

		Convey("ValueBin for same value should be deterministic", func() {
			value := BaseValue(42)
			b1 := value.Bin()
			b2 := value.Bin()
			So(b1, ShouldEqual, b2)
		})
	})
}

func TestFlatten(t *testing.T) {
	Convey("Given a value", t, func() {
		value := BaseValue('Z')

		Convey("When Flatten is called", func() {
			flat := value.Flatten()

			Convey("FlatValue count should match ActiveCount", func() {
				So(int(flat.Count), ShouldEqual, value.ActiveCount())
			})

			Convey("ActivePrimes indices should be within 257", func() {
				for idx := uint16(0); idx < flat.Count; idx++ {
					So(flat.ActivePrimes[idx], ShouldBeLessThan, 257)
				}
			})
		})
	})
}

func TestValuePrimeIndices(t *testing.T) {
	Convey("Given a value", t, func() {
		value := BaseValue(100)

		Convey("When ValuePrimeIndices is called", func() {
			indices := ValuePrimeIndices(&value)

			Convey("It should return count matching ActiveCount", func() {
				So(len(indices), ShouldEqual, value.ActiveCount())
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
			So(bits.OnesCount64(0), ShouldEqual, 0)
			So(bits.OnesCount64(1), ShouldEqual, 1)
			So(bits.OnesCount64(0xFF), ShouldEqual, 8)
			So(bits.OnesCount64(0xFFFFFFFFFFFFFFFF), ShouldEqual, 64)
		})
	})
}

func TestRollLeftPositional(t *testing.T) {
	Convey("Given a value and position", t, func() {
		value := BaseValue('M')

		Convey("When RollLeft is called for positional encoding", func() {
			bound := value.RollLeft(17)

			Convey("It should preserve active count", func() {
				So(bound.ActiveCount(), ShouldEqual, value.ActiveCount())
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
	values, err := genValuesFromBytes([][]byte{
		[]byte("first"),
		[]byte("second"),
		[]byte("third"),
		[]byte("A longer sequence to stress the value builder"),
	})

	Convey("Given values and a pool", t, func() {
		So(err, ShouldBeNil)
		workerPool := pool.New(context.Background(), 2, 4, pool.NewConfig())
		defer workerPool.Close()

		Convey("When FlattenBatched is called", func() {
			flats := FlattenBatched(values, workerPool)

			Convey("It should produce one FlatValue per input", func() {
				So(len(flats), ShouldEqual, len(values))
				for idx := range values {
					So(int(flats[idx].Count), ShouldEqual, values[idx].ActiveCount())
				}
			})
		})
	})
}

func BenchmarkValueRotationSeed(b *testing.B) {
	value := BaseValue('x')
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = value.RotationSeed()
	}
}

func BenchmarkBuildValue(b *testing.B) {
	payload := []byte("The quick brown fox jumps over the lazy dog.")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = BuildValue(payload)
	}
}

func BenchmarkValueLCM(b *testing.B) {
	values, err := genValuesFromBytes([][]byte{[]byte("a"), []byte("ab"), []byte("abc"), []byte("abcd"), []byte("abcde")})
	if err != nil {
		b.Fatalf("genValuesFromBytes: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Value{}.LCM(values)
	}
}

func BenchmarkFlattenBatched(b *testing.B) {
	values, err := genValuesFromBytes([][]byte{[]byte("first"), []byte("second"), []byte("third"), []byte("A longer sequence to stress the value builder")})
	if err != nil {
		b.Fatalf("genValuesFromBytes: %v", err)
	}

	workerPool := pool.New(context.Background(), 2, 4, pool.NewConfig())
	defer workerPool.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = FlattenBatched(values, workerPool)
	}
}

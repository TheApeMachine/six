package macro

import (
	"sync"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
)

/*
keyFromByte generates an AffineKey between b and b+1.
Note that for b=255, b+1 intentionally wraps to 0 in the 8-bit space,
producing a key that bridges the boundary from the end of the byte range
back to its start. This exercises the system's modular continuity.
*/
func keyFromByte(b byte) AffineKey {
	return AffineKeyFromValues(
		primitive.BaseValue(b),
		primitive.BaseValue(b+1),
	)
}

func TestMacroIndex(t *testing.T) {
	gc.Convey("Given a fresh MacroIndex", t, func() {
		idx := NewMacroIndexServer()

		gc.Convey("FindOpcode should return false for an unknown key", func() {
			_, found := idx.FindOpcode(keyFromByte(42))
			gc.So(found, gc.ShouldBeFalse)
		})

		gc.Convey("RecordOpcode should create a new opcode with UseCount 1", func() {
			key := keyFromByte(42)
			idx.RecordOpcode(key)

			op, found := idx.FindOpcode(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.Key, gc.ShouldResemble, key)
			gc.So(op.UseCount, gc.ShouldEqual, uint64(1))
			gc.So(op.Hardened, gc.ShouldBeFalse)
		})

		gc.Convey("RecordOpcode should increment UseCount on repeated recording", func() {
			key := keyFromByte(77)
			for range 4 {
				idx.RecordOpcode(key)
			}

			op, found := idx.FindOpcode(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.UseCount, gc.ShouldEqual, uint64(4))
			gc.So(op.Hardened, gc.ShouldBeFalse)
		})

		gc.Convey("RecordOpcode should harden after threshold (>5 uses)", func() {
			key := keyFromByte(99)
			for range 6 {
				idx.RecordOpcode(key)
			}

			op, found := idx.FindOpcode(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.UseCount, gc.ShouldEqual, uint64(6))
			gc.So(op.Hardened, gc.ShouldBeTrue)
		})

		gc.Convey("RecordOpcode at exactly 5 uses should NOT be hardened", func() {
			key := keyFromByte(88)
			for range 5 {
				idx.RecordOpcode(key)
			}

			op, _ := idx.FindOpcode(key)
			gc.So(op.UseCount, gc.ShouldEqual, uint64(5))
			gc.So(op.Hardened, gc.ShouldBeFalse)
		})

		gc.Convey("Different keys should create distinct opcodes", func() {
			key10 := keyFromByte(10)
			key20 := keyFromByte(20)
			idx.RecordOpcode(key10)
			idx.RecordOpcode(key20)

			op10, found10 := idx.FindOpcode(key10)
			op20, found20 := idx.FindOpcode(key20)

			gc.So(found10, gc.ShouldBeTrue)
			gc.So(found20, gc.ShouldBeTrue)
			gc.So(op10.Key, gc.ShouldNotResemble, op20.Key)
		})
	})
}

func TestMacroIndexAvailableHardened(t *testing.T) {
	gc.Convey("Given an index with mixed soft and hardened opcodes", t, func() {
		idx := NewMacroIndexServer()

		key10 := keyFromByte(10)
		key20 := keyFromByte(20)
		key30 := keyFromByte(30)

		// Soft opcode (2 uses)
		idx.RecordOpcode(key10)
		idx.RecordOpcode(key10)

		// Hardened opcode (7 uses)
		for range 7 {
			idx.RecordOpcode(key20)
		}

		// Another hardened opcode (8 uses)
		for range 8 {
			idx.RecordOpcode(key30)
		}

		gc.Convey("AvailableHardened should return only hardened opcodes", func() {
			tools := idx.AvailableHardened()
			gc.So(len(tools), gc.ShouldEqual, 2)

			keys := map[AffineKey]bool{}
			for _, tool := range tools {
				gc.So(tool.Hardened, gc.ShouldBeTrue)
				keys[tool.Key] = true
			}

			gc.So(keys[key20], gc.ShouldBeTrue)
			gc.So(keys[key30], gc.ShouldBeTrue)
		})

		gc.Convey("AvailableHardened on empty index should return nil", func() {
			empty := NewMacroIndexServer()
			gc.So(empty.AvailableHardened(), gc.ShouldBeNil)
		})
	})
}

func TestMacroIndexGarbageCollect(t *testing.T) {
	gc.Convey("Given an index with single-use and multi-use opcodes", t, func() {
		idx := NewMacroIndexServer()

		key1 := keyFromByte(1)
		key2 := keyFromByte(2)
		key3 := keyFromByte(3)
		key50 := keyFromByte(50)
		key100 := keyFromByte(100)

		// Single-use opcodes (should be pruned)
		idx.RecordOpcode(key1)
		idx.RecordOpcode(key2)
		idx.RecordOpcode(key3)

		// Multi-use opcode (should survive)
		idx.RecordOpcode(key50)
		idx.RecordOpcode(key50)

		// Hardened opcode (should survive)
		for range 10 {
			idx.RecordOpcode(key100)
		}

		gc.Convey("GarbageCollect should prune single-use opcodes and return count", func() {
			pruned := idx.GarbageCollect()
			gc.So(pruned, gc.ShouldEqual, 3)

			_, found1 := idx.FindOpcode(key1)
			_, found2 := idx.FindOpcode(key2)
			_, found3 := idx.FindOpcode(key3)
			gc.So(found1, gc.ShouldBeFalse)
			gc.So(found2, gc.ShouldBeFalse)
			gc.So(found3, gc.ShouldBeFalse)
		})

		gc.Convey("Multi-use and hardened opcodes should survive GC", func() {
			idx.GarbageCollect()

			op50, found50 := idx.FindOpcode(key50)
			gc.So(found50, gc.ShouldBeTrue)
			gc.So(op50.UseCount, gc.ShouldEqual, uint64(2))

			op100, found100 := idx.FindOpcode(key100)
			gc.So(found100, gc.ShouldBeTrue)
			gc.So(op100.Hardened, gc.ShouldBeTrue)
		})

		gc.Convey("Second GC pass on a clean index should prune zero", func() {
			idx.GarbageCollect()
			gc.So(idx.GarbageCollect(), gc.ShouldEqual, 0)
		})
	})
}

func TestMacroIndexConcurrency(t *testing.T) {
	gc.Convey("Given concurrent writers and readers", t, func() {
		idx := NewMacroIndexServer()

		gc.Convey("Concurrent RecordOpcode and FindOpcode should not race", func() {
			var wg sync.WaitGroup

			for goroutine := range 20 {
				wg.Add(1)

				go func(seed int) {
					defer wg.Done()

					key := keyFromByte(byte(seed % 50))

					for range 100 {
						idx.RecordOpcode(key)
						idx.FindOpcode(key)
						idx.AvailableHardened()
					}
				}(goroutine)
			}

			wg.Wait()

			gc.So(len(idx.AvailableHardened()), gc.ShouldBeGreaterThan, 0)
		})
	})
}

func TestMacroIndexProgramCandidates(t *testing.T) {
	gc.Convey("Given a MacroIndex tracking synthesized program candidates", t, func() {
		idx := NewMacroIndexServer()
		key := keyFromByte(123)

		gc.Convey("RecordCandidateResult should create a candidate and keep it transient after one success", func() {
			candidate := idx.RecordCandidateResult(key, 9, 3, true, true)

			gc.So(candidate, gc.ShouldNotBeNil)
			gc.So(candidate.Key, gc.ShouldResemble, key)
			gc.So(candidate.SuccessCount, gc.ShouldEqual, uint64(1))
			gc.So(candidate.FailureCount, gc.ShouldEqual, uint64(0))
			gc.So(candidate.PreResidue, gc.ShouldEqual, 9)
			gc.So(candidate.PostResidue, gc.ShouldEqual, 3)
			gc.So(candidate.Advanced, gc.ShouldBeTrue)
			gc.So(candidate.Stable, gc.ShouldBeTrue)

			opcode, found := idx.FindOpcode(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(opcode.Hardened, gc.ShouldBeFalse)
			gc.So(opcode.UseCount, gc.ShouldEqual, uint64(1))
		})

		gc.Convey("Repeated exact success should promote the candidate into a hardened opcode", func() {
			for range 6 {
				idx.RecordCandidateResult(key, 9, 1, true, true)
			}

			candidate, found := idx.FindCandidate(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(candidate.SuccessCount, gc.ShouldEqual, uint64(6))
			gc.So(candidate.FailureCount, gc.ShouldEqual, uint64(0))

			opcode, found := idx.FindOpcode(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(opcode.UseCount, gc.ShouldEqual, uint64(6))
			gc.So(opcode.Hardened, gc.ShouldBeTrue)
		})

		gc.Convey("Failed execution should accumulate failure evidence without promotion", func() {
			idx.RecordCandidateResult(key, 4, 4, false, false)
			idx.RecordCandidateResult(key, 4, 5, false, true)

			candidate, found := idx.FindCandidate(key)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(candidate.SuccessCount, gc.ShouldEqual, uint64(0))
			gc.So(candidate.FailureCount, gc.ShouldEqual, uint64(2))

			opcode, found := idx.FindOpcode(key)
			gc.So(found, gc.ShouldBeFalse)
			gc.So(opcode, gc.ShouldBeNil)
		})
	})
}

// --- Benchmarks ---

func BenchmarkRecordOpcode(b *testing.B) {
	idx := NewMacroIndexServer()

	keys := make([]AffineKey, 256)
	for iter := range 256 {
		keys[iter] = keyFromByte(byte(iter))
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		idx.RecordOpcode(keys[iter%256])
	}
}

func BenchmarkFindOpcode(b *testing.B) {
	idx := NewMacroIndexServer()

	keys := make([]AffineKey, 256)
	for iter := range 256 {
		keys[iter] = keyFromByte(byte(iter))
		idx.RecordOpcode(keys[iter])
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		idx.FindOpcode(keys[iter%256])
	}
}

func BenchmarkAvailableHardened(b *testing.B) {
	idx := NewMacroIndexServer()

	for shift := range 50 {
		key := keyFromByte(byte(shift))
		for range 10 {
			idx.RecordOpcode(key)
		}
	}

	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		idx.AvailableHardened()
	}
}

func BenchmarkGarbageCollect(b *testing.B) {
	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		b.StopTimer()

		idx := NewMacroIndexServer()

		for shift := range 200 {
			idx.RecordOpcode(keyFromByte(byte(shift)))
		}

		b.StartTimer()
		idx.GarbageCollect()
	}
}

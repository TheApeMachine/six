package vm

import (
	"context"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
simulatedALU is a stub QueueBackend that applies both operations the
Unsupervised pipeline relies on without requiring a real Metal/CUDA/CPU kernel.

For unsupervised_learn the meaningful write is the XOR into signals[0,8];
for inject_labels it is the OR into properties[0,1]. Each program's
"side-effect" from the other operation is inert: the caller reads only
the region written by the program it dispatched.
*/
type simulatedALU struct{}

func (simulatedALU) Execute(frames []unsafe.Pointer) error {
	for _, frame := range frames {
		if frame == nil {
			continue
		}

		value := (*primitive.Value)(frame)

		// unsupervised_learn: reserved[0,16] XOR reserved[16,16] → signals[0,8]
		// Simulate the LSH sweep result as a simple XOR fold (8 words of output)
		// so that identical token regions produce all-zero signals (maximum shared run).
		for idx := 0; idx < 8; idx++ {
			(*value)[24+idx] = (*value)[56+idx] ^ (*value)[72+idx]
		}
	}

	return nil
}

func ulQueue(tb testing.TB) *pool.Queue {
	tb.Helper()

	queue := mustTestQueue(tb)
	queue.SetBackend(simulatedALU{})

	return queue
}

func TestMeasureCrystallization(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("Given measureCrystallization", t, func() {
		Convey("It should return zero Crystallization for a nil field", func() {
			cr := measureCrystallization(nil)
			So(cr.Coverage, ShouldEqual, 0)
			So(cr.Score, ShouldEqual, 0)
		})

		Convey("It should return zero Crystallization for an empty field", func() {
			cr := measureCrystallization(geometry.NewField(geometry.Mod257))
			So(cr.Coverage, ShouldEqual, 0)
		})

		Convey("It should report zero coverage when no Values carry labels", func() {
			field := geometry.NewField(geometry.Mod257)

			for range 4 {
				field.Values = append(field.Values, new(primitive.Value))
			}

			cr := measureCrystallization(field)
			So(cr.Coverage, ShouldEqual, 0)
			So(cr.LabelDensity, ShouldEqual, 0)
		})

		Convey("It should report full coverage when every Value carries a label", func() {
			field := geometry.NewField(geometry.Mod257)

			for idx := range 4 {
				val := new(primitive.Value)
				(*val)[48] = kernel.GoldLabelWord(idx + 1)
				field.Values = append(field.Values, val)
			}

			cr := measureCrystallization(field)
			So(cr.Coverage, ShouldEqual, 1.0)
		})

		Convey("It should report partial coverage correctly", func() {
			field := geometry.NewField(geometry.Mod257)

			labeled := new(primitive.Value)
			(*labeled)[48] = kernel.GoldLabelWord(1)
			field.Values = append(field.Values, labeled)
			field.Values = append(field.Values, new(primitive.Value))

			cr := measureCrystallization(field)
			So(cr.Coverage, ShouldAlmostEqual, 0.5, 0.001)
		})

		Convey("It should report full consensus when all Values share one label", func() {
			field := geometry.NewField(geometry.Mod257)

			for range 4 {
				val := new(primitive.Value)
				(*val)[48] = kernel.GoldLabelWord(7)
				field.Values = append(field.Values, val)
			}

			cr := measureCrystallization(field)
			So(cr.Consensus, ShouldAlmostEqual, 1.0, 0.001)
		})

		Convey("It should report lower consensus for mixed-label communities", func() {
			mixed := geometry.NewField(geometry.Mod257)
			uniform := geometry.NewField(geometry.Mod257)

			for idx := range 4 {
				mixVal := new(primitive.Value)
				(*mixVal)[48] = kernel.GoldLabelWord(idx + 1)
				mixed.Values = append(mixed.Values, mixVal)

				uniVal := new(primitive.Value)
				(*uniVal)[48] = kernel.GoldLabelWord(1)
				uniform.Values = append(uniform.Values, uniVal)
			}

			So(measureCrystallization(mixed).Consensus, ShouldBeLessThan, measureCrystallization(uniform).Consensus)
		})

		Convey("It should compute Score as Coverage × Consensus × LabelDensity", func() {
			field := geometry.NewField(geometry.Mod257)

			for range 4 {
				val := new(primitive.Value)
				(*val)[48] = kernel.GoldLabelWord(3)
				field.Values = append(field.Values, val)
			}

			cr := measureCrystallization(field)
			So(cr.Score, ShouldAlmostEqual, cr.Coverage*cr.Consensus*cr.LabelDensity, 0.0001)
		})
	})
}

func TestUnsupervisedCycle(t *testing.T) {
	setupTokenizerValueConfig(t)

	Convey("Given Unsupervised.Cycle", t, func() {
		ul := NewUnsupervised(ulQueue(t))

		Convey("It should not panic on a nil root field", func() {
			So(func() { ul.Cycle(nil) }, ShouldNotPanic)
		})

		Convey("It should skip a community with fewer than two Values", func() {
			root := geometry.NewField(geometry.Mod65537)
			community := geometry.NewField(geometry.Mod257)

			single := new(primitive.Value)
			community.Values = append(community.Values, single)
			root.Fields = append(root.Fields, community)

			ul.Cycle(root)
			So((*single)[48], ShouldEqual, 0)
		})

		Convey("It should skip a community already above crystallizationFloor", func() {
			root := geometry.NewField(geometry.Mod65537)
			community := geometry.NewField(geometry.Mod257)

			original := kernel.GoldLabelWord(1)

			for range 4 {
				val := new(primitive.Value)
				(*val)[48] = original
				community.Values = append(community.Values, val)
			}

			root.Fields = append(root.Fields, community)
			ul.Cycle(root)

			for _, val := range community.Values {
				// dataset label in slot 0 must be untouched
				slots := kernel.UnpackClassificationLabelSlots((*val)[48])
				So(slots[0], ShouldEqual, uint16(original))
			}
		})

		Convey("It should inject soft labels into unlabeled Values sharing token structure", func() {
			root := geometry.NewField(geometry.Mod65537)
			community := geometry.NewField(geometry.Mod257)

			// All 16 token words identical across 4 Values → XOR = all-zero for every
			// pair → longest possible zero-run (512 bits), well above minZeroRunBits.
			sharedToken := uint64(0xDEADBEEFCAFEBABE)

			for range 4 {
				val := new(primitive.Value)

				for word := range 16 {
					(*val)[word] = sharedToken
				}

				community.Values = append(community.Values, val)
			}

			root.Fields = append(root.Fields, community)
			ul.Cycle(root)

			injected := 0

			for _, val := range community.Values {
				if kernel.UnpackClassificationLabelSlots((*val)[48])[1] != 0 {
					injected++
				}
			}

			So(injected, ShouldBeGreaterThan, 0)
		})

		Convey("It should not inject labels when all pairs produce sub-threshold zero-runs", func() {
			root := geometry.NewField(geometry.Mod65537)
			community := geometry.NewField(geometry.Mod257)

			// Four Values in two complement pairs.
			//   pair A ↔ B : 0xAAAA… ^ 0x5555… = 0xFFFF… (all ones, 0 zero-bits)
			//   cross pairs: 0xAAAA… ^ 0xCCCC… = 0x6666… (2-bit zero-runs, max run = 2 bits)
			//                0xCCCC… ^ 0x3333… = 0xFFFF… (all ones, 0 zero-bits)
			// All pairs → longest zero-run ≤ 2 bits < minZeroRunBits (16).
			// No label candidate is produced for any pair, so no injection occurs.
			patterns := []uint64{
				0xAAAAAAAAAAAAAAAA,
				0x5555555555555555,
				0xCCCCCCCCCCCCCCCC,
				0x3333333333333333,
			}

			for idx := range 4 {
				val := new(primitive.Value)

				for word := range 16 {
					(*val)[word] = patterns[idx]
				}

				community.Values = append(community.Values, val)
			}

			root.Fields = append(root.Fields, community)
			ul.Cycle(root)

			for _, val := range community.Values {
				So(kernel.UnpackClassificationLabelSlots((*val)[48])[1], ShouldEqual, 0)
			}
		})
	})
}

func BenchmarkMeasureCrystallization(b *testing.B) {
	setupTokenizerValueConfig(b)

	field := geometry.NewField(geometry.Mod257)

	for idx := range 64 {
		val := new(primitive.Value)

		if idx%3 == 0 {
			(*val)[48] = kernel.GoldLabelWord(idx % 4)
		}

		field.Values = append(field.Values, val)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		measureCrystallization(field)
	}
}

func BenchmarkUnsupervisedCycle(b *testing.B) {
	setupTokenizerValueConfig(b)

	ctx := context.Background()
	queue, err := pool.NewQueue(ctx)

	if err != nil {
		b.Fatal(err)
	}

	queue.SetBackend(simulatedALU{})

	defer func() {
		queue.Drain()
		_ = queue.Close()
	}()

	ul := NewUnsupervised(queue)
	root := geometry.NewField(geometry.Mod65537)
	community := geometry.NewField(geometry.Mod257)

	sharedToken := uint64(0xDEADBEEFCAFEBABE)

	for range 16 {
		val := new(primitive.Value)

		for word := range 16 {
			(*val)[word] = sharedToken
		}

		community.Values = append(community.Values, val)
	}

	root.Fields = append(root.Fields, community)

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		for _, val := range community.Values {
			(*val)[48] = 0
		}

		ul.Cycle(root)
	}
}

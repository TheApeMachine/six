package kernel

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestApplyCopyMaskMerge(t *testing.T) {
	Convey("Given ApplyCopyMaskMerge", t, func() {
		Convey("It should merge srcA into dst under srcB mask", func() {
			var frame [128]uint64
			frame[ProgramOpcodeWord] = OpcodeCopyMaskMerge
			frame[ProgramSrcAWord] = PackRegionRef(10, 2)
			frame[ProgramSrcBWord] = PackRegionRef(60, 2)
			frame[ProgramDstWord] = PackRegionRef(30, 2)

			frame[10] = 0xF0F0_F0F0_F0F0_F0F0
			frame[11] = 0x1234_5678_9ABC_DEF0
			frame[60] = ^uint64(0)
			frame[61] = ^uint64(0)
			frame[30] = 0
			frame[31] = 0

			ApplyCopyMaskMerge(&frame)

			So(frame[30], ShouldEqual, frame[10])
			So(frame[31], ShouldEqual, frame[11])
		})
	})
}

func TestApplyRefutationProbe(t *testing.T) {
	Convey("Given ApplyRefutationProbe", t, func() {
		Convey("It should mark noise and clear scheduling on strong one-run", func() {
			var frame [128]uint64
			frame[PropertiesRefutationTargetWord] = 99

			for idx := 0; idx < 8; idx++ {
				frame[SignalsStartWord+idx] = ^uint64(0)
			}

			ApplyRefutationProbe(&frame)

			So(frame[PropertiesNoiseWord]&FalsifiedBitNoiseWord, ShouldEqual, FalsifiedBitNoiseWord)
			So(frame[SchedulingNextProgramWord], ShouldEqual, uint64(0))
			So(frame[PropertiesRefutationTargetWord], ShouldEqual, uint64(0))
		})

		Convey("It should no-op when target is zero", func() {
			var frame [128]uint64

			for idx := 0; idx < 8; idx++ {
				frame[SignalsStartWord+idx] = ^uint64(0)
			}

			ApplyRefutationProbe(&frame)

			So(frame[PropertiesNoiseWord]&FalsifiedBitNoiseWord, ShouldEqual, uint64(0))
		})
	})
}

func TestApplyPostExecutionLifecycle(t *testing.T) {
	Convey("Given ApplyPostExecutionLifecycle", t, func() {
		Convey("It should emit sentinel when TTL drains from one", func() {
			var frame [128]uint64
			frame[PropertiesTTLWord] = 1
			frame[SchedulingNextProgramWord] = 42

			ApplyPostExecutionLifecycle(&frame)

			So(frame[PropertiesTTLWord], ShouldEqual, TTLExpiredSentinel)
			So(frame[SchedulingNextProgramWord], ShouldEqual, uint64(0))
		})

		Convey("It should decrement positive TTL", func() {
			var frame [128]uint64
			frame[PropertiesTTLWord] = 7

			ApplyPostExecutionLifecycle(&frame)

			So(frame[PropertiesTTLWord], ShouldEqual, uint64(6))
		})
	})
}

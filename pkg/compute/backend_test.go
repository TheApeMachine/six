package compute

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
)

func setupTestConfig(tb testing.TB) {
	tb.Helper()

	original := *core.Cfg
	tb.Cleanup(func() {
		*core.Cfg = original
	})

	core.Cfg.Value.Region.Program.Start = 76
	core.Cfg.Value.Region.Program.Bits = 3328
	core.Cfg.Value.Region.State.Accumulator = 62
	core.Cfg.Value.Region.State.Sequence = 61
	core.Cfg.Value.Region.Registers.FW = 74
	core.Cfg.Value.Region.Registers.PC = 75
	core.Cfg.System.BatchSize = 64
	core.Cfg.System.BatchWindow = 0
}

func makeTestBackend(t *testing.T) *Backend {
	t.Helper()
	setupTestConfig(t)

	pool, err := NewPool(
		PoolWithContext(t.Context()),
		PoolWithProcs(4),
		PoolWithJobBuffer(64),
	)

	if err != nil {
		t.Fatal(err)
	}

	backend := NewBackend(t.Context(), WithPool(pool))

	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}

	return backend
}

func TestQueueNilReturnsError(t *testing.T) {
	convey.Convey("Queue(nil) returns an error", t, func() {
		backend := makeTestBackend(t)

		err := backend.Queue(nil)
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func TestQueueAcceptsValidFrame(t *testing.T) {
	convey.Convey("Queue accepts a valid frame pointer", t, func() {
		backend := makeTestBackend(t)

		var f [128]uint64
		err := backend.Queue(unsafe.Pointer(&f))
		convey.So(err, convey.ShouldBeNil)
	})
}

type recordingSubstrate struct {
	calls [][]unsafe.Pointer
}

func (substrate *recordingSubstrate) UniversalBitwise(frames []unsafe.Pointer) error {
	substrate.calls = append(
		substrate.calls,
		append([]unsafe.Pointer(nil), frames...),
	)
	return nil
}

func (substrate *recordingSubstrate) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

func (*recordingSubstrate) Name() string {
	return "recording"
}

var _ kernel.Substrate = (*recordingSubstrate)(nil)

type errUniversalSubstrate struct{}

func (errUniversalSubstrate) UniversalBitwise([]unsafe.Pointer) error {
	return errors.New("substrate UniversalBitwise failed")
}

func (errUniversalSubstrate) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

func (errUniversalSubstrate) Name() string {
	return "err-universal"
}

type countingOKSubstrate struct {
	calls int
}

func (substrate *countingOKSubstrate) UniversalBitwise([]unsafe.Pointer) error {
	substrate.calls++

	return nil
}

func (countingOKSubstrate) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

func (countingOKSubstrate) Name() string {
	return "counting-ok"
}

var (
	_ kernel.Substrate = errUniversalSubstrate{}
	_ kernel.Substrate = (*countingOKSubstrate)(nil)
)

func TestExecuteBatchFallsBackWhenPreferredSubstrateErrors(t *testing.T) {
	convey.Convey("executeBatch runs UniversalBitwise on a later substrate when preferred errors", t, func() {
		setupTestConfig(t)

		good := &countingOKSubstrate{}
		backend := &Backend{
			ctx: context.Background(),
			hardware: []kernel.Substrate{
				errUniversalSubstrate{},
				good,
			},
			queues: map[QueueType]chan unsafe.Pointer{
				PRIORITY: make(chan unsafe.Pointer, 8),
				NORMAL:   make(chan unsafe.Pointer, 8),
			},
			batchSize:   1,
			batchWindow: 0,
		}
		backend.ensureHardwareMetrics()

		var frame [128]uint64
		frame[76] = 0x1111

		err := backend.executeBatch([]unsafe.Pointer{unsafe.Pointer(&frame)})
		convey.So(err, convey.ShouldBeNil)
		convey.So(good.calls, convey.ShouldEqual, 1)
	})
}

func TestGroupFramesByProgram(t *testing.T) {
	convey.Convey("Frames are grouped by identical program regions before dispatch", t, func() {
		backend := makeTestBackend(t)

		var frameA, frameB, frameC [128]uint64

		frameA[76] = 0x1111
		frameB[76] = 0x1111
		frameC[76] = 0x2222

		groups := backend.groupFramesByProgram([]unsafe.Pointer{
			unsafe.Pointer(&frameA),
			unsafe.Pointer(&frameB),
			unsafe.Pointer(&frameC),
		})

		convey.So(len(groups), convey.ShouldEqual, 2)
		convey.So(len(groups[0]), convey.ShouldEqual, 2)
		convey.So(len(groups[1]), convey.ShouldEqual, 1)
	})
}

func TestExecuteBatchDispatchesProgramGroups(t *testing.T) {
	convey.Convey("executeBatch dispatches homogeneous program groups separately", t, func() {
		setupTestConfig(t)

		substrate := &recordingSubstrate{}
		backend := &Backend{
			ctx:      context.Background(),
			hardware: []kernel.Substrate{substrate},
			queues: map[QueueType]chan unsafe.Pointer{
				PRIORITY: make(chan unsafe.Pointer, 8),
				NORMAL:   make(chan unsafe.Pointer, 8),
			},
		}
		backend.ensureHardwareMetrics()

		var frameA, frameB, frameC [128]uint64
		frameA[76] = 0x1111
		frameB[76] = 0x1111
		frameC[76] = 0x2222

		err := backend.executeBatch([]unsafe.Pointer{
			unsafe.Pointer(&frameA),
			unsafe.Pointer(&frameB),
			unsafe.Pointer(&frameC),
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(len(substrate.calls), convey.ShouldEqual, 2)
		convey.So(len(substrate.calls[0]), convey.ShouldEqual, 2)
		convey.So(len(substrate.calls[1]), convey.ShouldEqual, 1)
	})
}

func TestExecuteBatchSelectsLeastLoadedHardware(t *testing.T) {
	convey.Convey("executeBatch selects the least-loaded accelerator", t, func() {
		setupTestConfig(t)

		busy := &recordingSubstrate{}
		idle := &recordingSubstrate{}
		backend := &Backend{
			ctx:      context.Background(),
			hardware: []kernel.Substrate{busy, idle},
			queues: map[QueueType]chan unsafe.Pointer{
				PRIORITY: make(chan unsafe.Pointer, 8),
				NORMAL:   make(chan unsafe.Pointer, 8),
			},
		}
		backend.ensureHardwareMetrics()
		backend.hardwareState[0].inflight.Store(2)
		backend.hardwareState[0].emaServiceNanos.Store(20)
		backend.hardwareState[1].inflight.Store(1)
		backend.hardwareState[1].emaServiceNanos.Store(10)

		var frame [128]uint64
		frame[76] = 0x1111

		err := backend.executeBatch([]unsafe.Pointer{unsafe.Pointer(&frame)})

		convey.So(err, convey.ShouldBeNil)
		convey.So(len(busy.calls), convey.ShouldEqual, 0)
		convey.So(len(idle.calls), convey.ShouldEqual, 1)
	})
}

func TestEvolveProgramsInGroup(t *testing.T) {
	convey.Convey("evolveProgramsInGroup applies HomologousCrossover for adjacent pairs when ProgramEvolution is on", t, func() {
		setupTestConfig(t)

		originalEvolution := core.Cfg.System.ProgramEvolution
		core.Cfg.System.ProgramEvolution = true

		t.Cleanup(func() {
			core.Cfg.System.ProgramEvolution = originalEvolution
		})

		em := &EvolutionManager{}
		var frameRecipient, frameDonor [128]uint64
		progStart := core.Cfg.Value.Region.Program.Start
		nProgWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)

		for offset := 0; offset < nProgWords; offset++ {
			word := uint64(0x100 + offset)
			frameRecipient[progStart+offset] = word
			frameDonor[progStart+offset] = word
		}

		slot := firmware.ProgramPayloadFirst32BitSlot()
		r0 := uint16(core.Cfg.Value.Region.Registers.R0)
		tokenWord := uint16(core.Cfg.Value.Region.Tokens.Start)
		donorInstr := uint32(0x6) | (uint32(r0) << 4) | (uint32(tokenWord) << 18)

		firmware.SetInstructionSlot(&frameRecipient, slot, 0)
		firmware.SetInstructionSlot(&frameDonor, slot, donorInstr)

		em.evolveProgramsInGroup([]unsafe.Pointer{
			unsafe.Pointer(&frameRecipient),
			unsafe.Pointer(&frameDonor),
		})

		convey.So(firmware.InstructionSlot(&frameRecipient, slot), convey.ShouldEqual, donorInstr)
	})

	convey.Convey("evolveProgramsInGroup is a no-op when ProgramEvolution is off", t, func() {
		setupTestConfig(t)

		originalEvolution := core.Cfg.System.ProgramEvolution
		core.Cfg.System.ProgramEvolution = false

		t.Cleanup(func() {
			core.Cfg.System.ProgramEvolution = originalEvolution
		})

		em := &EvolutionManager{}
		var frameRecipient, frameDonor [128]uint64
		progStart := core.Cfg.Value.Region.Program.Start
		nProgWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)

		for offset := 0; offset < nProgWords; offset++ {
			word := uint64(0x100 + offset)
			frameRecipient[progStart+offset] = word
			frameDonor[progStart+offset] = word
		}

		slot := firmware.ProgramPayloadFirst32BitSlot()
		r0 := uint16(core.Cfg.Value.Region.Registers.R0)
		tokenWord := uint16(core.Cfg.Value.Region.Tokens.Start)
		donorInstr := uint32(0x6) | (uint32(r0) << 4) | (uint32(tokenWord) << 18)

		firmware.SetInstructionSlot(&frameRecipient, slot, 0)
		firmware.SetInstructionSlot(&frameDonor, slot, donorInstr)

		em.evolveProgramsInGroup([]unsafe.Pointer{
			unsafe.Pointer(&frameRecipient),
			unsafe.Pointer(&frameDonor),
		})

		convey.So(firmware.InstructionSlot(&frameRecipient, slot), convey.ShouldEqual, uint32(0))
	})
}

func BenchmarkEvolveProgramsInGroup(b *testing.B) {
	setupTestConfig(b)

	originalEvolution := core.Cfg.System.ProgramEvolution
	core.Cfg.System.ProgramEvolution = true

	b.Cleanup(func() {
		core.Cfg.System.ProgramEvolution = originalEvolution
	})

	em := &EvolutionManager{}

	var templateRecipient, templateDonor [128]uint64
	progStart := core.Cfg.Value.Region.Program.Start
	nProgWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)

	for offset := 0; offset < nProgWords; offset++ {
		word := uint64(0x100 + offset)
		templateRecipient[progStart+offset] = word
		templateDonor[progStart+offset] = word
	}

	slot := firmware.ProgramPayloadFirst32BitSlot()
	r0 := uint16(core.Cfg.Value.Region.Registers.R0)
	tokenWord := uint16(core.Cfg.Value.Region.Tokens.Start)
	donorInstr := uint32(0x6) | (uint32(r0) << 4) | (uint32(tokenWord) << 18)

	firmware.SetInstructionSlot(&templateRecipient, slot, 0)
	firmware.SetInstructionSlot(&templateDonor, slot, donorInstr)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		frameRecipient := templateRecipient
		frameDonor := templateDonor

		em.evolveProgramsInGroup([]unsafe.Pointer{
			unsafe.Pointer(&frameRecipient),
			unsafe.Pointer(&frameDonor),
		})
	}
}

func TestHandleFollowUpDoesNotMutateProgramRegion(t *testing.T) {
	convey.Convey("handleFollowUp only requeues frames; it does not rewrite their program bits", t, func() {
		setupTestConfig(t)

		backend := &Backend{
			ctx: context.Background(),
			queues: map[QueueType]chan unsafe.Pointer{
				PRIORITY: make(chan unsafe.Pointer, 8),
				NORMAL:   make(chan unsafe.Pointer, 8),
			},
		}

		var frame [128]uint64
		frame[core.Cfg.Value.Region.Registers.FW] = 7
		frame[76] = 0xDEADBEEF
		frame[0] = 1

		backend.handleFollowUp([]unsafe.Pointer{unsafe.Pointer(&frame)})

		convey.So(frame[76], convey.ShouldEqual, uint64(0xDEADBEEF))
		convey.So(len(backend.queues[PRIORITY]), convey.ShouldEqual, 1)
	})
}

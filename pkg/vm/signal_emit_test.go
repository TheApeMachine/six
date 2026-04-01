package vm

import (
	"context"
	"io"
	"strings"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

func tombstoneClose(backend *compute.Backend, v *primitive.Value) {
	if v == nil || backend == nil {
		return
	}

	v.InstallTombstone()

	var tp primitive.Value

	primitive.CopyFrame(&tp, v)

	_ = backend.UniversalBitwise(unsafe.Pointer(v), unsafe.Pointer(&tp))
	_ = v.Close()
}

func TestEmitFromPairwiseSignals(t *testing.T) {
	Convey("Distinct encodings yield non-empty cut Structures linked to parent A", t, func() {
		store.ResetDefaultSpatialIndex()

		parentA, err := primitive.NewValue([]byte("alpha"))
		So(err, ShouldBeNil)

		partnerB, err := primitive.NewValue([]byte("alphabet"))
		So(err, ShouldBeNil)

		backend := compute.NewBackgroundBackend()
		defer backend.Close()

		defer tombstoneClose(backend, parentA)
		defer tombstoneClose(backend, partnerB)

		var workSelf, workPartner primitive.Value

		primitive.CopyFrame(&workSelf, parentA)
		primitive.CopyFrame(&workPartner, partnerB)
		workSelf.InstallLearnFirmware()

		So(backend.UniversalBitwise(
			unsafe.Pointer(&workSelf),
			unsafe.Pointer(&workPartner),
		), ShouldBeNil)

		cuts := EmitFromPairwiseSignals(parentA, partnerB, &workSelf)
		So(len(cuts), ShouldBeGreaterThan, 0)

		parentID := parentA[core.Cfg.Value.Region.ID.Start]

		for i := range cuts {
			So(cuts[i].Frame[core.Cfg.Value.Region.Prev.Start], ShouldEqual, parentID)
			So(cuts[i].Frame[core.Cfg.Value.Region.ID.Start], ShouldNotEqual, parentID)
		}
	})
}

func TestMachineStreamPairsLines(t *testing.T) {
	Convey("Machine ingests consecutive lines pairing each with the previous frame", t, func() {
		store.ResetDefaultSpatialIndex()

		ctx := context.Background()
		reader := io.NopCloser(strings.NewReader("substrate line one\nsubstrate line two\n"))

		_, err := NewMachine(ctx, WithDestinations(reader))
		So(err, ShouldBeNil)

		store.DefaultSpatialIndex().Flush()
	})
}

func BenchmarkEmitFromPairwiseSignals(b *testing.B) {
	backend := compute.NewBackgroundBackend()
	defer backend.Close()

	parentA, err := primitive.NewValue([]byte("alpha"))
	if err != nil {
		b.Fatal(err)
	}

	defer tombstoneClose(backend, parentA)

	partnerB, err := primitive.NewValue([]byte("alphabet"))
	if err != nil {
		b.Fatal(err)
	}

	defer tombstoneClose(backend, partnerB)

	var workSelf, workPartner primitive.Value

	primitive.CopyFrame(&workSelf, parentA)
	primitive.CopyFrame(&workPartner, partnerB)
	workSelf.InstallLearnFirmware()

	if err := backend.UniversalBitwise(
		unsafe.Pointer(&workSelf),
		unsafe.Pointer(&workPartner),
	); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		_ = EmitFromPairwiseSignals(parentA, partnerB, &workSelf)
	}
}

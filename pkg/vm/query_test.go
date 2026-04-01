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

func TestResolvePromptIntersection(t *testing.T) {
	Convey("After ingest, prefix intersection finds every line that shares those byte positions", t, func() {
		store.ResetDefaultSpatialIndex()

		ctx := context.Background()
		corpus := "X: sandra garden\nX: roy kitchen\nX: harold kitchen\n"
		reader := io.NopCloser(strings.NewReader(corpus))

		_, err := NewMachine(ctx, WithDestinations(reader))
		So(err, ShouldBeNil)

		idx := store.DefaultSpatialIndex()
		idx.Flush()

		hits := ResolvePromptIntersection(idx, []byte("X: "))
		So(len(hits), ShouldBeGreaterThanOrEqualTo, 3)
		So(len(hits), ShouldBeGreaterThanOrEqualTo, 3)
	})
}

func TestPrevChainBackward(t *testing.T) {
	Convey("A signal-cut frame chains Prev to its canonical parent in the LSM", t, func() {
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

		first := cuts[0]
		first.RegisterDefaultLSM()

		idx := store.DefaultSpatialIndex()
		idx.Flush()

		cutID := first.Frame[core.Cfg.Value.Region.ID.Start]
		parentID := parentA[core.Cfg.Value.Region.ID.Start]

		chain := PrevChainBackward(idx, cutID, 8)
		So(len(chain), ShouldBeGreaterThan, 1)
		So(chain[0], ShouldEqual, cutID)
		So(chain[1], ShouldEqual, parentID)
	})
}

func BenchmarkResolvePromptIntersection(b *testing.B) {
	store.ResetDefaultSpatialIndex()

	ctx := context.Background()
	reader := io.NopCloser(strings.NewReader("X: a\nX: b\nX: c\n"))

	_, err := NewMachine(ctx, WithDestinations(reader))
	if err != nil {
		b.Fatal(err)
	}

	idx := store.DefaultSpatialIndex()
	idx.Flush()

	prompt := []byte("X: ")

	b.ResetTimer()

	for b.Loop() {
		_ = ResolvePromptIntersection(idx, prompt)
	}
}

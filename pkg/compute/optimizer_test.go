package compute

import (
	"context"
	"errors"
	"testing"
	"time"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestOptimizerWorkloadExecutesProgramOwnerOverCommunity(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"program": {Start: primitive.ProgramStartWord, Words: primitive.ProgramWords},
			"id":      {Start: primitive.IDStartWord, Words: primitive.IDWords},
			"out":     {Start: primitive.SignalsStartWord, Words: 1},
		},
	}
	compiled, err := program.Compile(`[ (out self) <= (id) <= community ]`, lay)
	if err != nil {
		t.Fatal(err)
	}

	owner := primitive.Emit()
	defer owner.Close()
	candidate := primitive.Emit()
	defer candidate.Close()
	ownerFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(owner))
	candidateFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(candidate))

	if !owner.InstallProgram(compiled.Words) {
		t.Fatal("install program failed")
	}

	NewOptimizer(context.Background(), []*primitive.Value{owner, candidate}).Workload()()

	if ownerFrame[primitive.SignalsStartWord] != owner.ID() {
		t.Fatalf("owner out = %d, want owner id %d", ownerFrame[primitive.SignalsStartWord], owner.ID())
	}
	if candidateFrame[primitive.SignalsStartWord] != owner.ID() {
		t.Fatalf("candidate out = %d, want owner id %d", candidateFrame[primitive.SignalsStartWord], owner.ID())
	}
	if owner.Status() != primitive.DONE {
		t.Fatalf("owner status = %d, want DONE", owner.Status())
	}
	if owner.SchedulingNext() != 0 {
		t.Fatalf("owner continuation = %d, want cleared", owner.SchedulingNext())
	}
	if !programCleared(owner) {
		t.Fatalf("owner program should be cleared after ALU processing")
	}
}

func TestOptimizerWorkloadSkipsSettledProgramOwner(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"id":  {Start: primitive.IDStartWord, Words: primitive.IDWords},
			"out": {Start: primitive.SignalsStartWord, Words: 1},
		},
	}
	compiled, err := program.Compile(`[ (out self) <= (id) <= community ]`, lay)
	if err != nil {
		t.Fatal(err)
	}

	owner := primitive.Emit()
	defer owner.Close()
	candidate := primitive.Emit()
	defer candidate.Close()
	candidateFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(candidate))

	if !owner.InstallProgram(compiled.Words) {
		t.Fatal("install program failed")
	}
	owner.SetSchedulingNext(0)

	NewOptimizer(context.Background(), []*primitive.Value{owner, candidate}).Workload()()

	if candidateFrame[primitive.SignalsStartWord] != 0 {
		t.Fatalf("candidate out = %d, want settled program to stay idle", candidateFrame[primitive.SignalsStartWord])
	}
}

func TestOptimizerWorkloadSchedulesReadyProgramOwnerOnly(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"id":      {Start: primitive.IDStartWord, Words: primitive.IDWords},
			"signals": {Start: primitive.SignalsStartWord, Words: 1},
		},
	}
	compiled, err := program.Compile(`[ (signals[0,1] self) <= (id) <= community ]`, lay)
	if err != nil {
		t.Fatal(err)
	}

	for _, status := range []primitive.StatusType{primitive.PENDING, primitive.BUSY, primitive.DONE} {
		func(status primitive.StatusType) {
			owner := primitive.Emit()
			defer owner.Close()

			candidate := primitive.Emit()
			defer candidate.Close()

			candidateFrame := (*[primitive.WordCount]uint64)(unsafe.Pointer(candidate))

			if !owner.InstallProgram(compiled.Words) {
				t.Fatal("install program failed")
			}
			owner.SetStatus(status)

			NewOptimizer(context.Background(), []*primitive.Value{owner, candidate}).Workload()()

			if candidateFrame[primitive.SignalsStartWord] != 0 {
				t.Fatalf("status %d candidate out = %d, want idle", status, candidateFrame[primitive.SignalsStartWord])
			}
		}(status)
	}
}

func TestOptimizerWorkloadMarksOwnerBusyDuringALU(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"signals":    {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
			"properties": {Start: primitive.PropertiesStartWord, Words: primitive.PropertiesWords},
		},
		Properties: map[string]int{
			"status": int(primitive.STATUS),
		},
	}
	compiled, err := program.Compile(`[ (signals[0,1] self) <= (properties.status) <= community ]`, lay)
	if err != nil {
		t.Fatal(err)
	}

	owner := primitive.Emit()
	defer owner.Close()

	if !owner.InstallProgram(compiled.Words) {
		t.Fatal("install program failed")
	}

	NewOptimizer(context.Background(), []*primitive.Value{owner}).Workload()()

	if owner.Get(primitive.SignalsRegion)[0] != uint64(primitive.BUSY) {
		t.Fatalf("owner observed status = %d, want BUSY", owner.Get(primitive.SignalsRegion)[0])
	}
	if owner.Status() != primitive.DONE {
		t.Fatalf("owner final status = %d, want DONE", owner.Status())
	}
}

func TestOptimizerWorkloadComparesOwnerAWithCandidateB(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"affinity": {Start: primitive.AffinityStartWord, Words: primitive.AffinityWords},
			"signals":  {Start: primitive.SignalsStartWord, Words: primitive.AffinityWords},
		},
	}
	compiled, err := program.Compile(`[ (signals[0,5] self) <= (affinity[0,5] ^ affinity[0,5]) <= community ]`, lay)
	if err != nil {
		t.Fatal(err)
	}

	owner := primitive.Emit()
	defer owner.Close()
	candidate := primitive.Emit()
	defer candidate.Close()
	owner.Set(primitive.AffinityStartWord, 0b1010)
	candidate.Set(primitive.AffinityStartWord, 0b1100)

	if !owner.InstallProgram(compiled.Words) {
		t.Fatal("install program failed")
	}

	NewOptimizer(context.Background(), []*primitive.Value{owner, candidate}).Workload()()

	got := candidate.Get(primitive.SignalsRegion)[0]
	if got != 0b0110 {
		t.Fatalf("candidate signal = %04b, want owner affinity XOR candidate affinity 0110", got)
	}
}

func TestBackendSubmitDispatchesHypercubeGossip(t *testing.T) {
	ctx := context.Background()
	backend := NewBackend(ctx)
	defer backend.Close()

	owner := primitive.Emit()
	defer owner.Close()
	compiled, err := program.Compile(`
	[ (properties.community B) <= (id[0,1] A) ]
	[ (properties.status self) <= (DONE) ]
	[ (properties.continuation emit) <= A ]`, program.Layout{
		Regions: map[string]program.RegionExtent{
			"properties": {Start: primitive.PropertiesStartWord, Words: primitive.PropertiesWords},
			"affinity":   {Start: primitive.AffinityStartWord, Words: primitive.AffinityWords},
			"id":         {Start: primitive.IDStartWord, Words: primitive.IDWords},
		},
		Properties: map[string]int{
			"community":    int(primitive.COMMUNITY),
			"status":       int(primitive.STATUS),
			"continuation": int(primitive.CONTINUATION),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.InstallProgram(compiled.Words) {
		t.Fatal("install recruit program failed")
	}
	accepted := primitive.Emit()
	defer accepted.Close()

	owner.Set(primitive.AffinityStartWord, 0b0001)
	accepted.Set(primitive.AffinityStartWord, 0b0011)
	accepted.SetStatus(primitive.READY)

	if err := backend.Submit(owner, []*primitive.Value{accepted}); err != nil {
		t.Fatal(err)
	}

	spawned := drainBackendSpawned(t, backend)
	defer primitive.CloseAll(spawned)

	if got, _ := owner.Property(primitive.COMMUNITY); got != owner.ID() {
		t.Fatalf("owner community = %d, want owner id %d", got, owner.ID())
	}
	if got, _ := accepted.Property(primitive.COMMUNITY); got != owner.ID() {
		t.Fatalf("accepted community = %d, want owner id %d", got, owner.ID())
	}
	if owner.Status() != primitive.DONE {
		t.Fatalf("owner status = %d, want DONE", owner.Status())
	}
	if len(spawned) == 0 {
		t.Fatalf("spawned len = %d, want at least 1", len(spawned))
	}
	if spawned[0].ID() == 0 || spawned[0].ID() == owner.ID() {
		t.Fatalf("spawned id = %d, owner id = %d", spawned[0].ID(), owner.ID())
	}
	if spawned[0].HasProgram() {
		t.Fatal("spawned value inherited a program")
	}
	if spawned[0].Status() != primitive.PENDING {
		t.Fatalf("spawned status = %d, want PENDING", spawned[0].Status())
	}
}

func TestBackendRunHypercubeGossipFallsBackAfterSubstrateError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cpuState := &substrateState{Substrate: cpu.NewBackend(ctx)}
	defer cpuState.Close()

	backend := &Backend{
		ctx:          ctx,
		cancel:       cancel,
		cpuSubstrate: cpuState,
		substrates: []*substrateState{
			{Substrate: failingSubstrate{}},
			cpuState,
		},
	}

	owner := primitive.Emit()
	defer owner.Close()
	accepted := primitive.Emit()
	defer accepted.Close()

	compiled, err := program.Compile(`[ (properties.community B) <= (id[0,1] A) ]`, program.Layout{
		Regions: map[string]program.RegionExtent{
			"properties": {Start: primitive.PropertiesStartWord, Words: primitive.PropertiesWords},
			"id":         {Start: primitive.IDStartWord, Words: primitive.IDWords},
		},
		Properties: map[string]int{
			"community": int(primitive.COMMUNITY),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.InstallProgram(compiled.Words) {
		t.Fatal("install fallback program failed")
	}

	backend.runHypercubeGossip(owner, []*primitive.Value{accepted})

	if got, _ := accepted.Property(primitive.COMMUNITY); got != owner.ID() {
		t.Fatalf("accepted community = %d, want owner id %d", got, owner.ID())
	}
	if owner.Status() != primitive.DONE {
		t.Fatalf("owner status = %d, want DONE after CPU fallback", owner.Status())
	}
}

func TestBackendRunHypercubeGossipKeepsProgramAfterSubstrateError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
		substrates: []*substrateState{
			{Substrate: failingSubstrate{}},
		},
	}

	owner := primitive.Emit()
	defer owner.Close()

	compiled, err := program.Compile(`[ (properties.community B) <= (id[0,1] A) ]`, program.Layout{
		Regions: map[string]program.RegionExtent{
			"properties": {Start: primitive.PropertiesStartWord, Words: primitive.PropertiesWords},
			"id":         {Start: primitive.IDStartWord, Words: primitive.IDWords},
		},
		Properties: map[string]int{
			"community": int(primitive.COMMUNITY),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.InstallProgram(compiled.Words) {
		t.Fatal("install error program failed")
	}

	backend.runHypercubeGossip(owner, []*primitive.Value{owner})

	if owner.Status() != primitive.ERROR {
		t.Fatalf("owner status = %d, want ERROR", owner.Status())
	}
	if programCleared(owner) {
		t.Fatal("owner program was cleared after failed substrate execution")
	}
}

func programCleared(value *primitive.Value) bool {
	if value == nil {
		return true
	}

	for _, word := range value.Get(primitive.ProgramRegion) {
		if word != 0 {
			return false
		}
	}

	return true
}

func drainBackendSpawned(t *testing.T, backend *Backend) []*primitive.Value {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var batch []*primitive.Value
		for value := range backend.Sync(context.Background()) {
			if value != nil {
				batch = append(batch, value)
			}
		}

		if len(batch) > 0 {
			return batch
		}

		time.Sleep(time.Millisecond)
	}

	return nil
}

func benchmarkCommunityProgramWords(b testing.TB) []uint64 {
	b.Helper()

	compiled, err := program.Compile(`[ (properties.community B) <= (id[0,1] A) ]`, program.Layout{
		Regions: map[string]program.RegionExtent{
			"properties": {Start: primitive.PropertiesStartWord, Words: primitive.PropertiesWords},
			"id":         {Start: primitive.IDStartWord, Words: primitive.IDWords},
		},
		Properties: map[string]int{
			"community": int(primitive.COMMUNITY),
		},
	})
	if err != nil {
		b.Fatal(err)
	}

	return compiled.Words
}

func BenchmarkBackendRunHypercubeGossipSyncCPUFallback(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cpuState := &substrateState{Substrate: cpu.NewBackend(ctx)}
	defer cpuState.Close()

	backend := &Backend{
		ctx:          ctx,
		cancel:       cancel,
		cpuSubstrate: cpuState,
		substrates: []*substrateState{
			{Substrate: failingSubstrate{}},
			cpuState,
		},
	}

	words := benchmarkCommunityProgramWords(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		owner := primitive.Emit()
		accepted := primitive.Emit()
		if !owner.InstallProgram(words) {
			b.Fatal("install program failed")
		}

		backend.runHypercubeGossip(owner, []*primitive.Value{accepted})
		for range backend.Sync(ctx) {
		}

		accepted.Close()
		owner.Close()
	}
}

func BenchmarkBackendRunHypercubeGossipSyncFailingSubstrate(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := &Backend{
		ctx:    ctx,
		cancel: cancel,
		substrates: []*substrateState{
			{Substrate: failingSubstrate{}},
		},
	}

	words := benchmarkCommunityProgramWords(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		owner := primitive.Emit()
		if !owner.InstallProgram(words) {
			b.Fatal("install program failed")
		}

		backend.runHypercubeGossip(owner, []*primitive.Value{owner})
		for range backend.Sync(ctx) {
		}

		owner.Close()
	}
}

type failingSubstrate struct{}

func (failingSubstrate) Name() string { return "failing" }

func (failingSubstrate) HypercubeGossip(
	*primitive.Value,
	[]*primitive.Value,
) ([]*primitive.Value, error) {
	return nil, errors.New("forced substrate failure")
}

func (failingSubstrate) GeometricFrame(unsafe.Pointer, uint64) bool { return false }

func (failingSubstrate) Close() error { return nil }

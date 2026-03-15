package vm

import (
	"context"
	"testing"
)

func TestMachineProjectionDisabledByDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	machine := NewMachine(
		MachineWithContext(ctx),
	)
	defer machine.Close()

	if machine.projection != ProjectionDisabled {
		t.Fatalf("projection: want disabled by default, got %v", machine.projection)
	}

	if machine.booter.parser.IsValid() {
		t.Fatalf("parser client should be invalid when projection is disabled")
	}
	if machine.booter.engine.IsValid() {
		t.Fatalf("engine client should be invalid when projection is disabled")
	}
	if machine.booter.cantilever.IsValid() {
		t.Fatalf("cantilever client should be invalid when projection is disabled")
	}
}

func TestMachineProjectionStagesBootExpectedOverlay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ingestOnly := NewMachine(
		MachineWithContext(ctx),
		MachineWithProjection(ProjectionIngest),
	)
	defer ingestOnly.Close()

	if !ingestOnly.booter.parser.IsValid() {
		t.Fatalf("parser client should be valid when ingest projection is enabled")
	}
	if !ingestOnly.booter.engine.IsValid() {
		t.Fatalf("engine client should be valid when ingest projection is enabled")
	}
	if ingestOnly.booter.cantilever.IsValid() {
		t.Fatalf("cantilever client should be invalid when only ingest projection is enabled")
	}

	promptOnly := NewMachine(
		MachineWithContext(ctx),
		MachineWithProjection(ProjectionPrompt),
	)
	defer promptOnly.Close()

	if !promptOnly.booter.parser.IsValid() {
		t.Fatalf("parser client should be valid when prompt projection is enabled")
	}
	if !promptOnly.booter.engine.IsValid() {
		t.Fatalf("engine client should be valid when prompt projection is enabled")
	}
	if !promptOnly.booter.cantilever.IsValid() {
		t.Fatalf("cantilever client should be valid when prompt projection is enabled")
	}
}

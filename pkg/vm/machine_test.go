package vm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

const recruitProgramForMachineTest = `
[ (signals[0,5] self) <= (context[0,5] ^ asset[40,5]) <= community ]
[ (properties.noise self) <= popcnt(signals[0,5]) <= community ]
[ (signals[7,1] self) <= (0) <= community ]
[ (signals[7,1] self) <= (1) ? (popcnt(signals[0,5]) | 120) <= community ]
[ (properties.community next) <= (properties.community) ? (signals[7,1] != 0) <= community ]
[ (context[0,5] self) <= (signals[0,5]) ? (signals[7,1] != 0) <= community ]
`

func installMachineTestRecruitProgram(t *testing.T, value *primitive.Value) {
	t.Helper()

	compiled, err := program.Compile(recruitProgramForMachineTest, program.Layout{
		Regions: map[string]program.RegionExtent{
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"properties": {Start: 56, Words: 16},
			"asset":      {Start: 72, Words: 48},
		},
		Properties: map[string]int{
			"noise":     int(primitive.NOISE),
			"community": int(primitive.COMMUNITY),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !value.InstallProgram(compiled.Words) {
		t.Fatal("failed to install recruit program")
	}
}

func TestMachineCycleRecruitsCommunityThroughProgramExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := compute.NewBackend(ctx)
	defer backend.Close()

	candidate := primitive.Emit()
	defer candidate.Close()
	candidate.SetProperty(primitive.CONTINUATION, candidate.ID())
	start, _ := primitive.AffinityRegion.WordExtent()
	candidate.Set(start, 0x3)
	candidate.NormalizeAffinity()

	root := primitive.Emit(primitive.WithRole(uint64(primitive.ValueRoleProgrammer)))
	defer root.Close()
	installMachineTestRecruitProgram(t, root)
	root.SetProperty(primitive.COMMUNITY, 99)

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		backend:   backend,
		community: []*primitive.Value{root, candidate},
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := machine.Cycle(); err != nil {
			t.Fatal(err)
		}

		got, _ := candidate.Property(primitive.COMMUNITY)
		if got == 99 {
			return
		}

		time.Sleep(time.Millisecond)
	}

	got, _ := candidate.Property(primitive.COMMUNITY)
	t.Fatalf("candidate community = %d, want recruited community 99", got)
}

func TestMachineCyclePublishesChangedTelemetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url, messages, closeServer := newMachineTelemetryServer(t)
	defer closeServer()

	enabled := core.Cfg.TelemetryEnabled
	defer func() {
		core.Cfg.TelemetryEnabled = enabled
	}()
	core.Cfg.TelemetryEnabled = true

	bridge, err := telemetry.NewBridge(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()

	backend := compute.NewBackend(ctx)
	defer backend.Close()

	value := primitive.Emit()
	defer value.Close()

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		backend:   backend,
		telemetry: bridge,
		community: []*primitive.Value{value},
	}

	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}
	if got := readMachineTelemetryMessage(t, messages); got != primitive.FrameByteLength {
		t.Fatalf("telemetry frame length = %d, want %d", got, primitive.FrameByteLength)
	}

	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}
	if !noMachineTelemetryMessage(messages) {
		t.Fatal("unchanged Value was resent")
	}

	value.SetProperty(primitive.NOISE, 1)
	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}
	if got := readMachineTelemetryMessage(t, messages); got != primitive.FrameByteLength {
		t.Fatalf("changed telemetry frame length = %d, want %d", got, primitive.FrameByteLength)
	}
}

func TestMachineCyclePrunesExpiredEphemeralValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := compute.NewBackend(ctx)
	defer backend.Close()

	compiled, err := program.Compile(`[ (signals[0,1] self) <= (id) <= community ]`, program.Layout{
		Regions: map[string]program.RegionExtent{
			"id":      {Start: primitive.IDStartWord, Words: primitive.IDWords},
			"signals": {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	value := primitive.Emit(primitive.WithTTL(1))
	if !value.InstallProgram(compiled.Words) {
		value.Close()
		t.Fatal("failed to install ephemeral program")
	}

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		backend:   backend,
		community: []*primitive.Value{value},
	}

	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}

	if len(machine.community) != 0 {
		primitive.CloseAll(machine.community)
		t.Fatalf("community len = %d, want expired ephemeral pruned", len(machine.community))
	}
}

func TestMachinePromptAppendsValuesBeforeCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := compute.NewBackend(ctx)
	defer backend.Close()

	value := primitive.Emit()
	defer value.Close()
	value.SetStatus(primitive.DONE)

	machine := &Machine{
		ctx:     ctx,
		cancel:  cancel,
		backend: backend,
	}

	resolved, err := machine.Prompt(value)
	if err != nil {
		t.Fatal(err)
	}

	if len(machine.community) != 1 {
		t.Fatalf("community len = %d, want 1", len(machine.community))
	}
	if len(resolved) != 1 || resolved[0] != value {
		t.Fatalf("resolved len = %d, want prompt Value resolved", len(resolved))
	}
}

func newMachineTelemetryServer(t *testing.T) (string, <-chan int, func()) {
	t.Helper()

	messages := make(chan int, 8)
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}

			messages <- len(payload)
		}
	}))

	return "ws" + strings.TrimPrefix(server.URL, "http"), messages, server.Close
}

func readMachineTelemetryMessage(t *testing.T, messages <-chan int) int {
	t.Helper()

	select {
	case length := <-messages:
		return length
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for telemetry message")
	}

	return 0
}

func noMachineTelemetryMessage(messages <-chan int) bool {
	select {
	case <-messages:
		return false
	case <-time.After(50 * time.Millisecond):
		return true
	}
}

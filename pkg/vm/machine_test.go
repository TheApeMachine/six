package vm

import (
	"context"
	"testing"
	"time"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
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

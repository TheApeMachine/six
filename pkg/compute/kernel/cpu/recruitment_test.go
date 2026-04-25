package cpu

import (
	"testing"

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

const recruitProgramSource = `
[ (signals[0,5] self) <= (context[0,5] ^ asset[40,5]) <= community ]
[ (properties.noise self) <= popcnt(signals[0,5]) <= community ]
[ (properties.falsified self) <= (0) <= community ]
[ (properties.falsified self) <= (1) ? (popcnt(signals[0,5]) | 120) <= community ]
[ (properties.community next) <= (properties.community) ? (properties.falsified != 0) <= community ]
`

func testLayout() program.Layout {
	return program.Layout{
		Regions: map[string]program.RegionExtent{
			"signals":    {Start: 32, Words: 8},
			"context":    {Start: 40, Words: 8},
			"properties": {Start: 56, Words: 16},
			"asset":      {Start: 72, Words: 48},
			"affinity":   {Start: 123, Words: 5},
		},
		Properties: map[string]int{
			"labels":       int(primitive.LABELS),
			"noise":        int(primitive.NOISE),
			"community":    int(primitive.COMMUNITY),
			"falsified":    int(primitive.FALSIFIED),
			"continuation": int(primitive.CONTINUATION),
		},
	}
}

func installRecruitProgram(t *testing.T, value *primitive.Value) {
	t.Helper()

	compiled, err := program.Compile(recruitProgramSource, testLayout())
	if err != nil {
		t.Fatal(err)
	}
	if !value.InstallProgram(compiled.Words) {
		t.Fatal("failed to install recruit program")
	}
}

func setAffinity(value *primitive.Value, words [primitive.AffinityWords]uint64) {
	start, _ := primitive.AffinityRegion.WordExtent()
	for i, word := range words {
		value.Set(start+i, word)
	}
	value.NormalizeAffinity()
}

func setContextFold(value *primitive.Value, words [primitive.AffinityWords]uint64) {
	start, _ := primitive.ContextRegion.WordExtent()
	for i, word := range words {
		value.Set(start+i, word)
	}
}

func TestRecruitCommunityProgramStampsNextCandidate(t *testing.T) {
	root := primitive.Emit()
	defer root.Close()
	candidate := primitive.Emit()
	defer candidate.Close()

	installRecruitProgram(t, root)
	root.SetProperty(primitive.COMMUNITY, 99)
	setContextFold(root, [primitive.AffinityWords]uint64{0x1})
	setAffinity(candidate, [primitive.AffinityWords]uint64{0x3})

	if _, err := root.Write(candidate.Bytes()); err != nil {
		t.Fatal(err)
	}

	HypercubeGossip(nil, []*primitive.Value{root, candidate})

	got, _ := candidate.Property(primitive.COMMUNITY)
	if got != 99 {
		t.Fatalf("candidate community = %d, want recruited community 99", got)
	}
}

func TestRecruitCommunityProgramRejectsSaturatedCandidate(t *testing.T) {
	root := primitive.Emit()
	defer root.Close()
	candidate := primitive.Emit()
	defer candidate.Close()

	installRecruitProgram(t, root)
	root.SetProperty(primitive.COMMUNITY, 99)
	setContextFold(root, [primitive.AffinityWords]uint64{
		^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0), 0x1,
	})
	setAffinity(candidate, [primitive.AffinityWords]uint64{})

	if _, err := root.Write(candidate.Bytes()); err != nil {
		t.Fatal(err)
	}

	HypercubeGossip(nil, []*primitive.Value{root, candidate})

	got, _ := candidate.Property(primitive.COMMUNITY)
	if got != 0 {
		t.Fatalf("candidate community = %d, want unassigned after saturation", got)
	}
}

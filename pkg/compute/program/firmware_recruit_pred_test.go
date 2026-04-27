package program

import (
	"context"
	"testing"
)

func TestRecruitCompoundPredicateStoresCommunityWord(t *testing.T) {
	t.Helper()

	ResetPredicateSession()

	ctx := context.Background()
	lay := Layout{
		Regions: map[string]RegionExtent{
			"affinity":   {Start: 123, Words: 5},
			"properties": {Start: 56, Words: 16},
		},
		Properties: map[string]int{
			"community": 8,
		},
		Opcodes: Opcodes,
	}
	src := `
program probe {
  target B where hamming(A.affinity, B.affinity) < 64 {
    when B.properties.community == 0 {
      write A.affinity <- or(A.affinity, B.affinity)
    }
  }
}
`
	_, err := compileFirmwareSource(ctx, src, lay)
	if err != nil {
		t.Fatal(err)
	}
	want := 56 + 8
	found := false
	for _, spec := range PredicateDeviceSpecs() {
		if spec.Kind != PredKindHammingLTAndScalarEq0 {
			continue
		}
		if int(spec.AndWord) == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no compound spec with AndWord %d", want)
	}
}

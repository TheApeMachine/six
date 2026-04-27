package program

import "testing"

func TestParseScalarCompareCommunityUsesLayoutIndex(t *testing.T) {
	t.Helper()

	comp := &compiler{
		lay: Layout{
			Regions: map[string]RegionExtent{
				"properties": {Start: 56, Words: 16},
			},
			Properties: map[string]int{
				"community": 8,
			},
		},
	}
	word, ne, err := comp.parseScalarCompare("B.properties.community == 0")
	if err != nil {
		t.Fatal(err)
	}
	if ne {
		t.Fatal("expected == 0 form")
	}
	if word != 56+8 {
		t.Fatalf("word index = %d, want %d", word, 56+8)
	}
}

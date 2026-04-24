package cpu

import (
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestExecuteCommunity_TopologyFold(t *testing.T) {
	lay := program.Layout{
		Regions: map[string]program.RegionExtent{
			"program": {Start: 16, Words: 16},
			"a":       {Start: 0, Words: 1},
			"dst":     {Start: 2, Words: 1},
		},
	}

	comp, err := program.Compile("[ (dst fold) <= (a ^ 0) <= community ]", lay)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	v1 := &primitive.Value{}
	v2 := &primitive.Value{}
	v3 := &primitive.Value{}

	f1 := (*[128]uint64)(unsafe.Pointer(v1))
	f2 := (*[128]uint64)(unsafe.Pointer(v2))
	f3 := (*[128]uint64)(unsafe.Pointer(v3))

	f1[16] = comp.Words[0]
	f2[16] = comp.Words[0]
	f3[16] = comp.Words[0]

	f1[0] = 0x1
	f2[0] = 0x2
	f3[0] = 0x4

	f1[2] = 0 // Initial dst

	ExecuteCommunity([]*primitive.Value{v1, v2, v3})

	if f1[2] != 0x7 {
		t.Fatalf("expected fold to accumulate 0x1 ^ 0x2 ^ 0x4 = 0x7, got %X", f1[2])
	}
	if f2[2] != 0x7 {
		t.Fatalf("expected f2.dst to become 0x7, got %X", f2[2])
	}
	if f3[2] != 0x7 {
		t.Fatalf("expected f3.dst to become 0x7, got %X", f3[2])
	}
}

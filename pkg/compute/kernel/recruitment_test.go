package kernel

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"
)

func TestBatchRecruitmentSaturation(t *testing.T) {
	folds := [][primitive.AffinityWords]uint64{
		{0x1},
		{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0), 0x1},
	}
	candidates := [][primitive.AffinityWords]uint64{
		{0x3},
		{},
	}

	got := BatchRecruitmentSaturation(folds, candidates, 0.47)
	want := []bool{false, true}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %t, want %t", i, got[i], want[i])
		}
	}
}

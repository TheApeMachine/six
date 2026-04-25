package metal

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"
)

func TestBatchRecruitmentSaturationMatchesGolden(t *testing.T) {
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
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %t, want %t", i, got[i], want[i])
		}
	}
}

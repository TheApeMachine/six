package program

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestRecruitCompoundPredicateStoresCommunityWord(t *testing.T) {
	Convey("Given a layout and Hamming+scalar guard firmware source", t, func() {
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

		Convey("When compiled", func() {
			_, err := compileFirmwareSource(ctx, src, lay)
			So(err, ShouldBeNil)

			Convey("Then a compound predicate stores the community word in AndWord", func() {
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
				So(found, ShouldBeTrue)
			})
		})
	})
}

func BenchmarkRecruitCompoundPredicateStoresCommunityWord(b *testing.B) {
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

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		ResetPredicateSession()
		_, _ = compileFirmwareSource(ctx, src, lay)
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
			b.Fatal("missing compound spec")
		}
	}
}

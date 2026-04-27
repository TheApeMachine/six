package program

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestParseScalarCompareCommunityUsesLayoutIndex(t *testing.T) {
	Convey("Given a compiler layout with a community property offset", t, func() {
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

		Convey("When parsing B.properties.community == 0", func() {
			word, notEqual, err := comp.parseScalarCompare("B.properties.community == 0")

			Convey("It should resolve the word index and equality form", func() {
				So(err, ShouldBeNil)
				So(notEqual, ShouldBeFalse)
				So(word, ShouldEqual, 56+8)
			})
		})
	})
}

func BenchmarkParseScalarCompareCommunity(b *testing.B) {
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
	const cond = "B.properties.community == 0"

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _, _ = comp.parseScalarCompare(cond)
	}
}

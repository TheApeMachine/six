package provider

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestAsyncTokens(t *testing.T) {
	gc.Convey("Given an async token producer", t, func() {
		out := AsyncTokens("test", func(tokens chan<- RawToken) {
			tokens <- RawToken{SampleID: 1, Symbol: 'a', Pos: 0}
			tokens <- RawToken{SampleID: 1, Symbol: 'b', Pos: 1}
		})

		var results []RawToken
		for token := range out {
			results = append(results, token)
		}

		gc.Convey("It should preserve token order and close the stream", func() {
			gc.So(len(results), gc.ShouldEqual, 2)
			gc.So(results[0], gc.ShouldResemble, RawToken{SampleID: 1, Symbol: 'a', Pos: 0})
			gc.So(results[1], gc.ShouldResemble, RawToken{SampleID: 1, Symbol: 'b', Pos: 1})
		})
	})
}

func BenchmarkAsyncTokens(b *testing.B) {
	for b.Loop() {
		out := AsyncTokens("bench", func(tokens chan<- RawToken) {
			tokens <- RawToken{SampleID: 1, Symbol: 'a', Pos: 0}
		})

		for range out {
		}
	}
}

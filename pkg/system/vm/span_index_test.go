package vm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

func TestSpanIndexResolve(t *testing.T) {
	gc.Convey("Given an exact span index over ingested samples", t, func() {
		spanIndex := NewSpanIndex()
		spanIndex.Ingest([]byte("The quick brown fox jumps over the lazy dog."))
		spanIndex.Ingest([]byte("The quick red fox jumps over the sleeping dog."))
		spanIndex.Ingest([]byte("The quick silver fox runs past the awake dog."))

		gc.Convey("it should return the longest remaining span for an exact shared prefix", func() {
			result := spanIndex.Resolve([]byte("The quick "))

			gc.So(string(result), gc.ShouldEqual, "red fox jumps over the sleeping dog.")
		})

		gc.Convey("it should break equal-length ties by ingestion order", func() {
			tied := NewSpanIndex()
			tied.Ingest([]byte("abcX"))
			tied.Ingest([]byte("abcY"))

			result := tied.Resolve([]byte("abc"))

			gc.So(string(result), gc.ShouldEqual, "X")
		})

		gc.Convey("it should return an exact miss when the query overruns the sample", func() {
			result := spanIndex.Resolve([]byte("The quick red fox jumps over the sleeping dog.!!!"))

			gc.So(len(result), gc.ShouldEqual, 0)
		})

		gc.Convey("it should return an empty continuation for a fully consumed sample", func() {
			result := spanIndex.Resolve([]byte("The quick red fox jumps over the sleeping dog."))

			gc.So(string(result), gc.ShouldEqual, "")
		})
	})
}

func BenchmarkSpanIndexResolve(b *testing.B) {
	spanIndex := NewSpanIndex()

	for _, sample := range []string{
		"The quick brown fox jumps over the lazy dog.",
		"The quick red fox jumps over the sleeping dog.",
		"The quick silver fox runs past the awake dog.",
		"If it rains and you have no umbrella you get wet.",
		"If you have an umbrella or stay inside you stay dry.",
	} {
		spanIndex.Ingest([]byte(sample))
	}

	b.ResetTimer()

	for b.Loop() {
		result := spanIndex.Resolve([]byte("The quick "))

		if len(result) == 0 {
			b.Fatal("result should not be empty")
		}
	}
}

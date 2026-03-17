package tokenizer

import (
	"math/rand"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func generateCorpus(paragraphs int, rng *rand.Rand) string {
	sentences := []string{
		"The quick brown fox jumps over the lazy dog.",
		"Alice was beginning to get very tired of sitting by her sister on the bank.",
		"In a hole in the ground there lived a hobbit.",
		"It was the best of times, it was the worst of times.",
		"Call me Ishmael.",
		"All happy families are alike; each unhappy family is unhappy in its own way.",
	}

	var builder strings.Builder

	for paragraph := range paragraphs {
		count := 3 + rng.Intn(5)

		for sentence := range count {
			builder.WriteString(sentences[rng.Intn(len(sentences))])

			if sentence < count-1 {
				builder.WriteByte(' ')
			}
		}

		if paragraph < paragraphs-1 {
			builder.WriteString("\n\n")
		}
	}

	return builder.String()
}

func generateBinaryNoise(size int, rng *rand.Rand) []byte {
	out := make([]byte, size)

	for i := range out {
		out[i] = byte(rng.Intn(256))
	}

	return out
}

func TestWrite(t *testing.T) {
	Convey("Given a UniversalServer", t, func() {
		tokenizer := NewUniversalServer(
			UniversalWithContext(t.Context()),
		)

		Convey("When writing a byte", func() {
			err := tokenizer.Write(t.Context(), Universal_write{})

			Convey("Then there should be no error", func() {
				So(err, ShouldBeNil)
			})
		})
	})
}

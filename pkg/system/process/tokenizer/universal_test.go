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

func newServer(t *testing.T) *UniversalServer {
	return NewUniversalServer(
		UniversalWithContext(t.Context()),
	)
}

func TestTokenizeSingleByte(t *testing.T) {
	Convey("Given a UniversalServer", t, func() {
		server := newServer(t)

		Convey("When tokenizing a single byte", func() {
			server.tokenize('A')

			Convey("It should not panic and sequences remain empty until a boundary fires", func() {
				So(server.sequences, ShouldBeEmpty)
			})
		})
	})
}

func TestTokenizeRepeatedPattern(t *testing.T) {
	Convey("Given a UniversalServer", t, func() {
		server := newServer(t)

		Convey("When feeding a repeated digram so a boundary fires", func() {
			// Sequitur fires a boundary on the second occurrence of 'ab'.
			server.tokenize('a')
			server.tokenize('b')
			server.tokenize('a')
			server.tokenize('b')

			Convey("It should accumulate at least one sequence", func() {
				So(len(server.sequences), ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestTokenizeCorpusPreservesBytes(t *testing.T) {
	Convey("Given a UniversalServer", t, func() {
		server := newServer(t)
		rng := rand.New(rand.NewSource(42))
		input := []byte(generateCorpus(10, rng))

		Convey("When tokenizing a realistic multi-paragraph corpus", func() {
			for _, b := range input {
				server.tokenize(b)
			}

			remaining := server.healer.Flush()

			Convey("It should reconstruct every byte without loss", func() {
				var got []byte

				for _, seq := range server.sequences {
					got = append(got, seq...)
				}

				for _, seq := range remaining {
					got = append(got, seq...)
				}

				So(len(got), ShouldEqual, len(input))

				for idx, b := range got {
					So(b, ShouldEqual, input[idx])
				}
			})
		})
	})
}

func TestTokenizeBinaryNoisePreservesBytes(t *testing.T) {
	Convey("Given a UniversalServer", t, func() {
		server := newServer(t)
		rng := rand.New(rand.NewSource(99))
		input := generateBinaryNoise(512, rng)

		Convey("When tokenizing 512 bytes of random binary noise", func() {
			for _, b := range input {
				server.tokenize(b)
			}

			remaining := server.healer.Flush()

			Convey("It should reconstruct every byte without loss", func() {
				var got []byte

				for _, seq := range server.sequences {
					got = append(got, seq...)
				}

				for _, seq := range remaining {
					got = append(got, seq...)
				}

				So(len(got), ShouldEqual, len(input))

				for idx, b := range got {
					So(b, ShouldEqual, input[idx])
				}
			})
		})
	})
}

func BenchmarkTokenizeByte(b *testing.B) {
	server := NewUniversalServer(
		UniversalWithContext(b.Context()),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		server.tokenize('x')
	}
}

func BenchmarkTokenizeCorpus(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	corpus := []byte(generateCorpus(10, rng))

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		server := NewUniversalServer(
			UniversalWithContext(b.Context()),
		)

		for _, byteVal := range corpus {
			server.tokenize(byteVal)
		}
	}
}

func BenchmarkTokenizeBinaryNoise(b *testing.B) {
	rng := rand.New(rand.NewSource(99))
	noise := generateBinaryNoise(512, rng)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		server := NewUniversalServer(
			UniversalWithContext(b.Context()),
		)

		for _, byteVal := range noise {
			server.tokenize(byteVal)
		}
	}
}

package tokenizer

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/pool"
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

func newServer(ctx context.Context, workerPool *pool.Pool) *UniversalServer {
	return NewUniversalServer(
		UniversalWithContext(ctx),
		UniversalWithPool(workerPool),
	)
}

func TestTokenizerServer(t *testing.T) {
	Convey("Given a UniversalServer", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 2, 16, nil)
		server := newServer(ctx, workerPool)

		Convey("When tokenizing natural language", func() {
			corpus := generateCorpus(3, rand.New(rand.NewSource(42)))
			chords, err := server.tokenize(ctx, []byte(corpus))

			Convey("It should complete without error", func() {
				So(err, ShouldBeNil)
			})

			Convey("It should produce at least one chord", func() {
				So(err, ShouldBeNil)
				So(len(chords), ShouldBeGreaterThan, 0)
			})

			Convey("Every chord should be non-zero", func() {
				So(err, ShouldBeNil)

				for _, chord := range chords {
					So(chord.Chord.ActiveCount(), ShouldBeGreaterThan, 0)
				}
			})
		})

		Convey("When tokenizing binary noise", func() {
			noise := generateBinaryNoise(512, rand.New(rand.NewSource(99)))
			chords, err := server.tokenize(ctx, noise)

			Convey("It should handle the full byte range without error", func() {
				So(err, ShouldBeNil)
				So(len(chords), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When tokenizing a single byte", func() {
			chords, err := server.tokenize(ctx, []byte("x"))

			Convey("It should produce exactly one chord", func() {
				So(err, ShouldBeNil)
				So(len(chords), ShouldEqual, 1)
				So(chords[0].Chord.ActiveCount(), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When processChunk encodes a known byte sequence", func() {
			chunk := []byte("hello")
			chord, err := server.processChunk(chunk, data.Chord{})

			Convey("It should return a non-zero chord without error", func() {
				So(err, ShouldBeNil)
				So(chord.ActiveCount(), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When Done is called", func() {
			err := server.Done(ctx, Universal_done{})

			Convey("It should return nil", func() {
				So(err, ShouldBeNil)
			})
		})
	})
}

func BenchmarkTokenizerGenerate(b *testing.B) {
	corpus := generateCorpus(10, rand.New(rand.NewSource(42)))

	b.Run("natural_language", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			server := newServer(ctx, p)
			_, _ = server.tokenize(ctx, []byte(corpus))
			cancel()
		}
	})

	noise := generateBinaryNoise(4096, rand.New(rand.NewSource(99)))

	b.Run("binary_noise", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			server := newServer(ctx, p)
			_, _ = server.tokenize(ctx, noise)
			cancel()
		}
	})

	rng := rand.New(rand.NewSource(42))
	for _, size := range []int{64, 256, 1024, 4096} {
		size := size
		chunk := make([]byte, size)

		for i := range chunk {
			chunk[i] = byte(rng.Intn(256))
		}

		b.Run(fmt.Sprintf("processChunk_%d", size), func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := pool.New(ctx, 1, 4, nil)
			server := newServer(ctx, p)
			b.ResetTimer()

			for range b.N {
				if _, err := server.processChunk(chunk, data.Chord{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

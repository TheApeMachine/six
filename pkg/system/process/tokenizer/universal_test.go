package tokenizer

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
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
	gc.Convey("Given a UniversalServer", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 2, 16, nil)
		server := newServer(ctx, workerPool)
		morton := data.NewMortonCoder()

		gc.Convey("When tokenizing natural language", func() {
			corpus := generateCorpus(3, rand.New(rand.NewSource(42)))
			keys := server.tokenize([]byte(corpus))

			gc.Convey("It should produce one key per byte", func() {
				gc.So(len(keys), gc.ShouldEqual, len(corpus))
			})

			gc.Convey("Every key should unpack to a valid byte", func() {
				for i, key := range keys {
					_, symbol := morton.Unpack(key)
					gc.So(symbol, gc.ShouldEqual, corpus[i])
				}
			})
		})

		gc.Convey("When tokenizing binary noise", func() {
			noise := generateBinaryNoise(512, rand.New(rand.NewSource(99)))
			keys := server.tokenize(noise)

			gc.Convey("It should handle the full byte range", func() {
				gc.So(len(keys), gc.ShouldEqual, len(noise))
			})
		})

		gc.Convey("When tokenizing a single byte", func() {
			keys := server.tokenize([]byte("x"))

			gc.Convey("It should produce exactly one key", func() {
				gc.So(len(keys), gc.ShouldEqual, 1)

				_, symbol := morton.Unpack(keys[0])
				gc.So(symbol, gc.ShouldEqual, byte('x'))
			})
		})

		gc.Convey("When Done is called", func() {
			err := server.Done(ctx, Universal_done{})

			gc.Convey("It should return nil", func() {
				gc.So(err, gc.ShouldBeNil)
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
			_ = server.tokenize([]byte(corpus))
			cancel()
		}
	})

	noise := generateBinaryNoise(4096, rand.New(rand.NewSource(99)))

	b.Run("binary_noise", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			server := newServer(ctx, p)
			_ = server.tokenize(noise)
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

		b.Run(fmt.Sprintf("tokenize_%d", size), func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			p := pool.New(ctx, 1, 4, nil)
			server := newServer(ctx, p)
			b.ResetTimer()

			for range b.N {
				_ = server.tokenize(chunk)
			}
		})
	}
}

func TestTokenizerUsesBoundaryLocalCellIndex(t *testing.T) {
	gc.Convey("Given a short byte stream", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 4, nil)
		server := newServer(ctx, workerPool)
		morton := data.NewMortonCoder()

		keys := server.tokenize([]byte("ababc"))

		gc.Convey("It should emit one key per byte", func() {
			gc.So(len(keys), gc.ShouldEqual, 5)
		})

		gc.Convey("Keys should encode the correct byte values", func() {
			_, sym0 := morton.Unpack(keys[0])
			_, sym1 := morton.Unpack(keys[1])
			_, sym2 := morton.Unpack(keys[2])
			_, sym3 := morton.Unpack(keys[3])
			_, sym4 := morton.Unpack(keys[4])

			gc.So(sym0, gc.ShouldEqual, byte('a'))
			gc.So(sym1, gc.ShouldEqual, byte('b'))
			gc.So(sym2, gc.ShouldEqual, byte('a'))
			gc.So(sym3, gc.ShouldEqual, byte('b'))
			gc.So(sym4, gc.ShouldEqual, byte('c'))
		})

		gc.Convey("Position encoding should start at 0", func() {
			pos0, _ := morton.Unpack(keys[0])
			gc.So(pos0, gc.ShouldEqual, uint32(0))
		})
	})
}

func TestTokenizerDeterministic(t *testing.T) {
	gc.Convey("Given repeated tokenization calls for the same input", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 4, nil)
		server := newServer(ctx, workerPool)

		first := server.tokenize([]byte("a"))
		second := server.tokenize([]byte("a"))

		gc.Convey("They should produce identical keys", func() {
			gc.So(len(first), gc.ShouldEqual, 1)
			gc.So(len(second), gc.ShouldEqual, 1)
			gc.So(first[0], gc.ShouldEqual, second[0])
		})
	})
}

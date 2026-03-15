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

func TestTokenizerUsesBoundaryLocalCellIndex(t *testing.T) {
	Convey("Given a short byte stream", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 4, nil)
		server := newServer(ctx, workerPool)

		edges, err := server.tokenize(ctx, []byte("ababc"))

		Convey("It should emit one edge per byte while resetting the local depth at boundaries", func() {
			So(err, ShouldBeNil)
			So(len(edges), ShouldEqual, 5)
			So(edges[0].Left, ShouldEqual, 'a')
			So(edges[0].Position, ShouldEqual, uint32(0))
			So(edges[1].Left, ShouldEqual, 'b')
			So(edges[1].Position, ShouldEqual, uint32(1))
			So(edges[2].Left, ShouldEqual, 'a')
			So(edges[2].Position, ShouldEqual, uint32(2))
			So(edges[3].Left, ShouldEqual, 'b')
			So(edges[3].Position, ShouldEqual, uint32(3))
			So(edges[4].Left, ShouldEqual, 'c')
			So(edges[4].Position, ShouldEqual, uint32(0))
		})

		Convey("Boundary bytes should reset the local traversal program and the final byte should halt", func() {
			So(err, ShouldBeNil)
			So(edges[3].Chord.Opcode(), ShouldEqual, uint64(data.OpcodeReset))
			So(edges[4].Chord.Terminal(), ShouldBeTrue)
			So(edges[4].Chord.Opcode(), ShouldEqual, uint64(data.OpcodeHalt))
		})
	})
}

func TestTokenizerResetsPhasePerGenerateCall(t *testing.T) {
	Convey("Given repeated tokenization calls for the same byte", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 4, nil)
		server := newServer(ctx, workerPool)

		first, errFirst := server.tokenize(ctx, []byte("a"))
		second, errSecond := server.tokenize(ctx, []byte("a"))

		Convey("They should start from the same identity phase each time", func() {
			So(errFirst, ShouldBeNil)
			So(errSecond, ShouldBeNil)
			So(len(first), ShouldEqual, 1)
			So(len(second), ShouldEqual, 1)

			calc := server.calc
			expected := calc.Multiply(1, calc.Power(3, uint32('a')))
			So(first[0].Chord.ResidualCarry(), ShouldEqual, uint64(expected))
			So(second[0].Chord.ResidualCarry(), ShouldEqual, uint64(expected))
		})
	})
}

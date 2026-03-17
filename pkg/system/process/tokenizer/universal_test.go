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

/*
tokenizeAll feeds each byte of raw through the server's streaming
tokenize method, collecting one key per byte.
*/
func tokenizeAll(server *UniversalServer, raw []byte) []uint64 {
	keys := make([]uint64, len(raw))

	for i, b := range raw {
		keys[i] = server.tokenize(b)
	}

	return keys
}

func fragmentedSequence(chunks []string) [][]byte {
	sequence := make([][]byte, len(chunks))

	for i, chunk := range chunks {
		sequence[i] = []byte(chunk)
	}

	return sequence
}

func assertKeyChunks(expected []string, keys []uint64) error {
	morton := data.NewMortonCoder()
	actual := []string{}
	current := make([]byte, 0)
	var expectedPos uint32 = 1

	for _, key := range keys {
		pos, symbol := morton.Unpack(key)

		if pos == 1 && len(current) > 0 && expectedPos != 1 {
			actual = append(actual, string(current))
			current = current[:0]
			expectedPos = 1
		}

		if pos != expectedPos {
			return fmt.Errorf("expected position %d, got %d", expectedPos, pos)
		}

		current = append(current, symbol)
		expectedPos++
	}

	if len(current) > 0 {
		actual = append(actual, string(current))
	}

	if len(actual) != len(expected) {
		return fmt.Errorf("expected %d chunks, got %d: %v vs %v", len(expected), len(actual), expected, actual)
	}

	for i := range actual {
		if actual[i] != expected[i] {
			return fmt.Errorf("chunk %d mismatch: expected %q, got %q", i, expected[i], actual[i])
		}
	}

	return nil
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
			keys := tokenizeAll(server, []byte(corpus))

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
			keys := tokenizeAll(server, noise)

			gc.Convey("It should handle the full byte range", func() {
				gc.So(len(keys), gc.ShouldEqual, len(noise))
			})
		})

		gc.Convey("When tokenizing a single byte", func() {
			key := server.tokenize('x')

			gc.Convey("It should unpack to the correct symbol", func() {
				_, symbol := morton.Unpack(key)
				gc.So(symbol, gc.ShouldEqual, byte('x'))
			})
		})

		gc.Convey("When Done is called", func() {
			future, release := server.client.Done(ctx, func(p Universal_done_Params) error {
				return nil
			})
			defer release()

			_, err := future.Struct()

			gc.Convey("It should return nil", func() {
				gc.So(err, gc.ShouldBeNil)
			})
		})
	})
}

func TestTokenizerServerBitwiseEmission(t *testing.T) {
	gc.Convey("Given a UniversalServer with buffered bitwise healing", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 4, nil)
		server := newServer(ctx, workerPool)

		gc.Convey("When two Roy sequences are finalized", func() {
			server.sequence = fragmentedSequence([]string{
				"Roy was ",
				"in t",
				"he liv",
				"ing ",
				"ro",
				"om",
			})

			keys, err := server.finalizeSequence(false)

			gc.So(err, gc.ShouldBeNil)
			gc.So(keys, gc.ShouldBeEmpty)

			server.sequence = fragmentedSequence([]string{
				"Roy is ",
				"i",
				"n the k",
				"itche",
				"n",
			})

			keys, err = server.finalizeSequence(false)

			gc.So(err, gc.ShouldBeNil)
			gc.So(assertKeyChunks([]string{"Roy wa", "s in the ", "living room"}, keys), gc.ShouldBeNil)

			keys, err = server.drainSequences(true)

			gc.So(err, gc.ShouldBeNil)
			gc.So(assertKeyChunks([]string{"Roy i", "s in the ", "kitchen"}, keys), gc.ShouldBeNil)
		})

		gc.Convey("When different overlap families are interleaved", func() {
			server.sequence = fragmentedSequence([]string{"Image of ", "ca", "t ", "is a ", "ca", "t"})
			keys, err := server.finalizeSequence(false)

			gc.So(err, gc.ShouldBeNil)
			gc.So(keys, gc.ShouldBeEmpty)

			server.sequence = fragmentedSequence([]string{"Roy was ", "in t", "he liv", "ing ", "ro", "om"})
			keys, err = server.finalizeSequence(false)

			gc.So(err, gc.ShouldBeNil)
			gc.So(keys, gc.ShouldBeEmpty)

			server.sequence = fragmentedSequence([]string{"Roy is ", "i", "n the k", "itche", "n"})
			keys, err = server.finalizeSequence(false)

			gc.So(err, gc.ShouldBeNil)
			gc.So(assertKeyChunks([]string{"Roy wa", "s in the ", "living room"}, keys), gc.ShouldBeNil)

			server.sequence = fragmentedSequence([]string{"Image of ", "do", "g ", "is a ", "do", "g"})
			keys, err = server.finalizeSequence(false)

			gc.So(err, gc.ShouldBeNil)
			gc.So(assertKeyChunks([]string{"Image of ", "cat", " is a ", "cat"}, keys), gc.ShouldBeNil)

			keys, err = server.drainSequences(true)

			gc.So(err, gc.ShouldBeNil)
			gc.So(assertKeyChunks(
				[]string{"Roy i", "s in the ", "kitchen", "Image of ", "dog", " is a ", "dog"},
				keys,
			), gc.ShouldBeNil)
		})
	})
}

func BenchmarkTokenizerWrite(b *testing.B) {
	corpus := generateCorpus(10, rand.New(rand.NewSource(42)))
	raw := []byte(corpus)

	b.Run("natural_language", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			server := newServer(ctx, p)

			for _, byt := range raw {
				server.tokenize(byt)
			}

			cancel()
		}
	})

	noise := generateBinaryNoise(4096, rand.New(rand.NewSource(99)))

	b.Run("binary_noise", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			server := newServer(ctx, p)

			for _, byt := range noise {
				server.tokenize(byt)
			}

			cancel()
		}
	})

	rng := rand.New(rand.NewSource(42))

	for _, size := range []int{64, 256, 1024, 4096} {
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
				for _, byt := range chunk {
					server.tokenize(byt)
				}
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

		keys := tokenizeAll(server, []byte("ababc"))

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

		gc.Convey("Position encoding should start at 1", func() {
			pos0, _ := morton.Unpack(keys[0])
			gc.So(pos0, gc.ShouldEqual, uint32(1))
		})
	})
}

func TestTokenizerDeterministic(t *testing.T) {
	gc.Convey("Given repeated tokenization calls for the same byte", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 4, nil)
		serverA := newServer(ctx, workerPool)
		serverB := newServer(ctx, workerPool)

		first := serverA.tokenize('a')
		second := serverB.tokenize('a')

		gc.Convey("They should produce identical keys", func() {
			gc.So(first, gc.ShouldEqual, second)
		})
	})
}

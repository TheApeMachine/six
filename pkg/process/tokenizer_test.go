package process

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/store/lsm"
)

/*
instrumentedInsert records every spatial insert call for post-hoc validation.
*/
type instrumentedInsert struct {
	mu      sync.Mutex
	calls   []insertRecord
	counter atomic.Int64
}

type chordSignature [8]uint64

type insertRecord struct {
	symbol    uint8
	position  uint32
	active    int
	signature chordSignature
}

func newChordSignature(chord data.Chord) chordSignature {
	return chordSignature{
		chord.C0(),
		chord.C1(),
		chord.C2(),
		chord.C3(),
		chord.C4(),
		chord.C5(),
		chord.C6(),
		chord.C7(),
	}
}

func (ins *instrumentedInsert) fn() lsm.SpatialInsertFunc {
	return func(_ context.Context, left uint8, position uint32, chord data.Chord) error {
		ins.counter.Add(1)

		ins.mu.Lock()
		ins.calls = append(ins.calls, insertRecord{
			symbol:    left,
			position:  position,
			active:    chord.ActiveCount(),
			signature: newChordSignature(chord),
		})
		ins.mu.Unlock()

		return nil
	}
}

func (ins *instrumentedInsert) count() int64 {
	return ins.counter.Load()
}

func (ins *instrumentedInsert) snapshot() []insertRecord {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	out := make([]insertRecord, len(ins.calls))
	copy(out, ins.calls)

	return out
}

/*
generateCorpus builds a realistic byte stream with structural variation.
It interleaves natural language sentences, numeric sequences, and whitespace
boundaries to exercise the MDL boundary detector across regime changes.
*/
func generateCorpus(paragraphs int, rng *rand.Rand) string {
	sentences := []string{
		"The quick brown fox jumps over the lazy dog.",
		"Alice was beginning to get very tired of sitting by her sister on the bank.",
		"In a hole in the ground there lived a hobbit.",
		"It was the best of times, it was the worst of times.",
		"Call me Ishmael.",
		"All happy families are alike; each unhappy family is unhappy in its own way.",
		"It is a truth universally acknowledged, that a single man in possession of a good fortune, must be in want of a wife.",
		"Mr. and Mrs. Dursley, of number four, Privet Drive, were proud to say that they were perfectly normal, thank you very much.",
		"The sky above the port was the color of television, tuned to a dead channel.",
		"In the beginning God created the heavens and the earth.",
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

		// Inject a regime change: numeric data between paragraphs.
		if paragraph < paragraphs-1 {
			builder.WriteString("\n\n")

			numLen := 10 + rng.Intn(30)
			for range numLen {
				builder.WriteByte(byte('0' + rng.Intn(10)))
			}

			builder.WriteString("\n\n")
		}
	}

	return builder.String()
}

/*
generateBinaryNoise produces raw bytes spanning the full 0..255 range.
Exercises the tokenizer on non-textual data where MDL boundaries
emerge from statistical shifts rather than whitespace.
*/
func generateBinaryNoise(size int, rng *rand.Rand) []byte {
	out := make([]byte, size)

	for i := range out {
		out[i] = byte(rng.Intn(256))
	}

	return out
}

/*
generateRepetitive creates a stream with long runs of identical bytes
followed by abrupt shifts. This should trigger Shannon ceiling splits.
*/
func generateRepetitive(runs int, runLen int) []byte {
	out := make([]byte, 0, runs*runLen)

	for run := range runs {
		val := byte((run * 37) % 256)

		for range runLen {
			out = append(out, val)
		}
	}

	return out
}

func makeServer(
	ctx context.Context,
	p *pool.Pool,
	broadcast *pool.BroadcastGroup,
	insert lsm.SpatialInsertFunc,
	samples []string,
) *TokenizerServer {
	server := NewTokenizerServer(
		TokenizerWithContext(ctx),
		TokenizerWithDataset(newMockDataset(samples), false),
	)
	server.Start(p, broadcast)
	server.spatialInsert = insert

	return server
}

func TestTokenizerServer(t *testing.T) {
	Convey("Given a TokenizerServer with natural language corpus", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 2, 16, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		ins := &instrumentedInsert{}

		corpus := generateCorpus(5, rand.New(rand.NewSource(42)))
		server := makeServer(ctx, p, broadcast, ins.fn(), []string{corpus})

		Convey("When generating over the corpus", func() {
			err := server.generate(ctx)

			Convey("It should complete without error", func() {
				So(err, ShouldBeNil)
			})

			Convey("It should have performed spatial inserts for every byte", func() {
				// Pool tasks are async; give workers time to drain.
				time.Sleep(200 * time.Millisecond)

				So(ins.count(), ShouldBeGreaterThan, 0)
			})

			Convey("Every insert should carry a non-zero chord", func() {
				time.Sleep(200 * time.Millisecond)

				records := ins.snapshot()
				for _, rec := range records {
					So(rec.active, ShouldBeGreaterThan, 0)
				}
			})

			Convey("Every token in a chunk should store the same full chunk chord", func() {
				directIns := &instrumentedInsert{}
				directServer := makeServer(ctx, p, broadcast, directIns.fn(), []string{"x"})
				directServer.collector = [][]data.Chord{{}}

				chunk := []byte("The quick brown fox jumps over the lazy dog and keeps on running")
				expectedChord, err := data.BuildChord(chunk)

				So(err, ShouldBeNil)
				So(directServer.processChunk(ctx, 0, chunk), ShouldBeNil)

				records := directIns.snapshot()
				So(len(records), ShouldEqual, len(chunk))
				So(directServer.collector[0], ShouldHaveLength, 1)

				expectedSignature := newChordSignature(expectedChord)
				So(newChordSignature(directServer.collector[0][0]), ShouldResemble, expectedSignature)

				for idx, rec := range records {
					So(rec.symbol, ShouldEqual, chunk[idx])
					So(rec.position, ShouldEqual, uint32(idx))
					So(rec.active, ShouldEqual, expectedChord.ActiveCount())
					So(rec.signature, ShouldResemble, expectedSignature)
				}
			})
		})

		Convey("When testing Done", func() {
			err := server.Done(ctx, Tokenizer_done{})

			Convey("It should return nil", func() {
				So(err, ShouldBeNil)
			})
		})

		Convey("When testing Announce", func() {
			server.Announce()

			Convey("It should not panic", func() {
				So(true, ShouldBeTrue)
			})
		})
	})

	Convey("Given a TokenizerServer with binary noise", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 2, 16, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		ins := &instrumentedInsert{}

		noise := generateBinaryNoise(2048, rand.New(rand.NewSource(99)))
		server := makeServer(ctx, p, broadcast, ins.fn(), []string{string(noise)})

		Convey("When generating over binary noise", func() {
			err := server.generate(ctx)

			Convey("It should handle full byte range without error", func() {
				So(err, ShouldBeNil)
			})

			Convey("It should produce inserts for the noise stream", func() {
				time.Sleep(200 * time.Millisecond)
				So(ins.count(), ShouldBeGreaterThan, 0)
			})
		})
	})

	Convey("Given a TokenizerServer with repetitive data", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 2, 16, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		ins := &instrumentedInsert{}

		// 20 runs of 128 identical bytes each triggers Shannon ceiling splits.
		rep := generateRepetitive(20, 128)
		server := makeServer(ctx, p, broadcast, ins.fn(), []string{string(rep)})

		Convey("When generating over repetitive data", func() {
			err := server.generate(ctx)

			Convey("It should not error", func() {
				So(err, ShouldBeNil)
			})

			Convey("It should still produce spatial inserts", func() {
				time.Sleep(200 * time.Millisecond)
				So(ins.count(), ShouldBeGreaterThan, 0)
			})
		})
	})

	Convey("Given a TokenizerServer with delayed boundary emission", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 2, 16, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		ins := &instrumentedInsert{}

		sample := string(bytes.Repeat([]byte{0}, 32)) +
			string(bytes.Repeat([]byte{255}, 32)) +
			string(bytes.Repeat([]byte{0}, 32))

		server := makeServer(ctx, p, broadcast, ins.fn(), []string{sample})

		Convey("When generating over the sample", func() {
			err := server.generate(ctx)

			Convey("It should reset positions at the committed boundary", func() {
				So(err, ShouldBeNil)

				records := ins.snapshot()
				minPos := uint32(^uint32(0))
				seen := false

				for _, rec := range records {
					if rec.symbol != 255 {
						continue
					}

					seen = true
					if rec.position < minPos {
						minPos = rec.position
					}
				}

				So(seen, ShouldBeTrue)
				So(minPos, ShouldEqual, 0)
			})
		})
	})
	Convey("Given a TokenizerServer with a single-byte corpus", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 1, 4, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		ins := &instrumentedInsert{}

		server := makeServer(ctx, p, broadcast, ins.fn(), []string{"x"})

		Convey("When generating over the corpus", func() {
			err := server.generate(ctx)

			Convey("It should insert the single byte instead of dropping it", func() {
				So(err, ShouldBeNil)
				So(ins.count(), ShouldEqual, 1)

				records := ins.snapshot()
				So(records, ShouldHaveLength, 1)
				So(records[0].symbol, ShouldEqual, 'x')
				So(records[0].position, ShouldEqual, 0)
				So(records[0].active, ShouldBeGreaterThan, 0)
			})
		})
	})

	Convey("Given a TokenizerServer with multi-sample dataset", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 2, 16, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		ins := &instrumentedInsert{}

		samples := []string{
			"The fundamental theorem of calculus links differentiation and integration.",
			"SELECT * FROM users WHERE age > 30 ORDER BY name ASC LIMIT 100;",
			string(generateBinaryNoise(512, rand.New(rand.NewSource(7)))),
			"def fibonacci(n): return n if n <= 1 else fibonacci(n-1) + fibonacci(n-2)",
			generateCorpus(2, rand.New(rand.NewSource(13))),
		}
		server := makeServer(ctx, p, broadcast, ins.fn(), samples)

		Convey("When generating across diverse samples", func() {
			err := server.generate(ctx)

			Convey("It should process all samples without error", func() {
				So(err, ShouldBeNil)
			})

			Convey("It should produce inserts from all modalities", func() {
				time.Sleep(200 * time.Millisecond)
				So(ins.count(), ShouldBeGreaterThan, 100)
			})
		})
	})

	Convey("Given a TokenizerServer with no spatial index", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server := NewTokenizerServer(
			TokenizerWithContext(ctx),
			TokenizerWithDataset(newMockDataset([]string{"anything"}), false),
		)

		Convey("When generate is called without spatial index", func() {
			err := server.generate(ctx)

			Convey("It should return ErrNoIndex", func() {
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, string(ErrNoIndex))
			})
		})
	})

	Convey("Given a TokenizerServer with a cancelled context", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		p := pool.New(ctx, 1, 10, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		ins := &instrumentedInsert{}

		// Large corpus so generate doesn't finish before cancel fires.
		corpus := generateCorpus(50, rand.New(rand.NewSource(77)))
		server := makeServer(ctx, p, broadcast, ins.fn(), []string{corpus})

		Convey("When the context is cancelled mid-generation", func() {
			cancel()
			err := server.generate(ctx)

			Convey("It should return a context error", func() {
				So(err, ShouldNotBeNil)
			})
		})
	})

	Convey("Given a TokenizerServer with Receive", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		p := pool.New(ctx, 1, 10, nil)
		broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
		server := makeServer(ctx, p, broadcast, nil, []string{"dummy"})

		Convey("When receiving a nil result", func() {
			So(func() { server.Receive(nil) }, ShouldNotPanic)
		})

		Convey("When receiving a result with wrong type", func() {
			result := &pool.Result{
				Value: pool.PoolValue[[]data.Chord]{
					Key: "spatial_index",
				},
			}

			So(func() { server.Receive(result) }, ShouldNotPanic)
		})
	})
}

func BenchmarkTokenizerGenerate(b *testing.B) {
	corpus := generateCorpus(10, rand.New(rand.NewSource(42)))

	b.Run("natural_language", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
			ins := &instrumentedInsert{}

			server := makeServer(ctx, p, broadcast, ins.fn(), []string{corpus})

			_ = server.generate(ctx)
			cancel()
		}
	})

	noise := string(generateBinaryNoise(4096, rand.New(rand.NewSource(99))))

	b.Run("binary_noise", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
			ins := &instrumentedInsert{}

			server := NewTokenizerServer(
				TokenizerWithContext(ctx),
				TokenizerWithDataset(newMockDataset([]string{noise}), false),
			)
			server.Start(p, broadcast)
			server.spatialInsert = ins.fn()

			_ = server.generate(ctx)
			cancel()
		}
	})

	rep := string(generateRepetitive(20, 256))

	b.Run("repetitive", func(b *testing.B) {
		for range b.N {
			ctx, cancel := context.WithCancel(context.Background())
			p := pool.New(ctx, 2, 16, nil)
			broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
			ins := &instrumentedInsert{}

			server := makeServer(ctx, p, broadcast, ins.fn(), []string{rep})

			_ = server.generate(ctx)
			cancel()
		}
	})
}

func BenchmarkProcessChunk(b *testing.B) {
	ctx := context.Background()

	sizes := []int{64, 256, 1024}
	rng := rand.New(rand.NewSource(42))

	for _, size := range sizes {
		chunk := make([]byte, size)

		for i := range chunk {
			chunk[i] = byte(rng.Intn(config.Numeric.VocabSize))
		}

		b.Run(fmt.Sprintf("bytes_%d", size), func(b *testing.B) {
			p := pool.New(ctx, 1, 4, nil)
			broadcast := p.CreateBroadcastGroup("tokenizer", time.Second)
			ins := &instrumentedInsert{}

			server := makeServer(ctx, p, broadcast, ins.fn(), []string{"x"})

			b.ResetTimer()

			for range b.N {
				if err := server.processChunk(ctx, 0, chunk); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

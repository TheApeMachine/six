package transport

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"runtime"
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

const benchmarkCopyBufSize = 32 * 1024

/*
payloadGenerator yields pseudo-random byte slices for round-trip tests.
The sequence is fixed for a given seed so failures reproduce.
*/
type payloadGenerator struct {
	random *rand.Rand
}

/*
newPayloadGenerator constructs a generator with a deterministic seed.
*/
func newPayloadGenerator(seed int64) *payloadGenerator {
	return &payloadGenerator{
		random: rand.New(rand.NewSource(seed)),
	}
}

/*
fillBytes writes the same pseudo-random sequence as repeated Uint32-to-byte draws
without allocating. length zero is a no-op.
*/
func (generator *payloadGenerator) fillBytes(destination []byte) {
	for index := range destination {
		destination[index] = byte(generator.random.Uint32())
	}
}

/*
roundTripCases builds edge-case payloads plus pseudo-random sizes for table tests.
*/
func roundTripCases(generator *payloadGenerator, randomPayloads int) []struct {
	label string
	data  []byte
} {
	edgeLengths := []int{
		0,
		1,
		15,
		16,
		1023,
		1024,
		4095,
		4096,
		4097,
		1 << 16,
	}

	randLens := make([]int, randomPayloads)

	for index := range randomPayloads {
		randLens[index] = 1 + generator.random.Intn(1<<14)
	}

	totalBytes := 0

	for _, length := range edgeLengths {
		if length > 0 {
			totalBytes += length
		}
	}

	for _, length := range randLens {
		totalBytes += length
	}

	blob := make([]byte, totalBytes)
	offset := 0

	cases := make([]struct {
		label string
		data  []byte
	}, 0, len(edgeLengths)+randomPayloads)

	for _, length := range edgeLengths {
		sample := []byte(nil)

		if length > 0 {
			sample = blob[offset : offset+length]
			offset += length

			generator.fillBytes(sample)
		}

		cases = append(cases, struct {
			label string
			data  []byte
		}{
			label: "edge_len_" + strconv.Itoa(length),
			data:  sample,
		})
	}

	for index, length := range randLens {
		sample := blob[offset : offset+length]
		offset += length

		generator.fillBytes(sample)

		cases = append(cases, struct {
			label string
			data  []byte
		}{
			label: "rand_" + strconv.Itoa(index) + "_len_" + strconv.Itoa(length),
			data:  sample,
		})
	}

	return cases
}

func TestBackendWriteReadRoundTrip(t *testing.T) {
	generator := newPayloadGenerator(0x6b637075)
	cases := roundTripCases(generator, 24)

	Convey("Given a Stream", t, func() {
		for _, testCase := range cases {
			label := testCase.label

			Convey("It should round-trip "+label, func() {
				stream := NewStream()
				out := new(bytes.Buffer)

				readDone := make(chan error, 1)

				var readBack int64

				go func() {
					var readErr error

					readBack, readErr = io.Copy(out, stream)
					readDone <- readErr
				}()

				written, writeErr := io.Copy(stream, bytes.NewReader(testCase.data))
				So(writeErr, ShouldBeNil)
				So(written, ShouldEqual, int64(len(testCase.data)))

				So(stream.Close(), ShouldBeNil)
				So(<-readDone, ShouldBeNil)
				So(readBack, ShouldEqual, int64(len(testCase.data)))
				So(bytes.Equal(out.Bytes(), testCase.data), ShouldBeTrue)

				closeErr := stream.Close()
				So(closeErr, ShouldBeNil)
			})
		}
	})
}

func TestStreamReset(t *testing.T) {
	Convey("Given a Stream that completed one payload", t, func() {
		payload := []byte("reuse")
		stream := NewStream()
		out := new(bytes.Buffer)
		readErr := make(chan error, 1)
		readKick := make(chan struct{})

		go func() {
			for range readKick {
				_, err := io.Copy(out, stream)
				readErr <- err
			}
		}()

		defer close(readKick)

		for round := 0; round < 2; round++ {
			out.Reset()

			readKick <- struct{}{}

			written, writeErr := io.Copy(stream, bytes.NewReader(payload))
			So(writeErr, ShouldBeNil)
			So(written, ShouldEqual, int64(len(payload)))

			So(stream.Close(), ShouldBeNil)
			So(<-readErr, ShouldBeNil)
			So(bytes.Equal(out.Bytes(), payload), ShouldBeTrue)
		}

		So(stream.Close(), ShouldBeNil)
	})
}

func TestStreamCloseWriteIdempotent(t *testing.T) {
	Convey("Given a Stream", t, func() {
		stream := NewStream()
		So(stream.Close(), ShouldBeNil)
		So(stream.Close(), ShouldBeNil)
	})
}

func TestWithOperationsRegistersHandlers(t *testing.T) {
	Convey("Given WithOperations option", t, func() {
		sink := NewSink()
		stream := NewStream(WithOperations(sink))
		So(stream, ShouldNotBeNil)
		So(len(stream.operations), ShouldEqual, 1)
		So(stream.Close(), ShouldBeNil)
	})
}

/*
invertOp is a test operation that XORs every byte with 0xFF.
Write accepts data, Read returns the inverted form.
*/
type invertOp struct {
	buf bytes.Buffer
}

func (op *invertOp) Write(p []byte) (n int, err error) {
	tmp := make([]byte, len(p))
	copy(tmp, p)

	for idx := range tmp {
		tmp[idx] ^= 0xFF
	}

	return op.buf.Write(tmp)
}

func (op *invertOp) Read(p []byte) (n int, err error) {
	return op.buf.Read(p)
}

func (op *invertOp) Close() error { return nil }

func TestOperationsTransformData(t *testing.T) {
	Convey("Given a Stream with an invert operation", t, func() {
		op := &invertOp{}
		stream := NewStream(WithOperations(op))

		input := []byte("hello")
		expected := make([]byte, len(input))

		for idx, byt := range input {
			expected[idx] = byt ^ 0xFF
		}

		Convey("It should apply the operation to the data", func() {
			out := new(bytes.Buffer)
			readDone := make(chan error, 1)

			go func() {
				_, readErr := io.Copy(out, stream)
				readDone <- readErr
			}()

			written, writeErr := io.Copy(stream, bytes.NewReader(input))
			So(writeErr, ShouldBeNil)
			So(written, ShouldEqual, int64(len(input)))

			So(stream.Close(), ShouldBeNil)
			So(<-readDone, ShouldBeNil)
			So(bytes.Equal(out.Bytes(), expected), ShouldBeTrue)
		})
	})
}

func TestOperationsFireEachCycle(t *testing.T) {
	Convey("Given a Stream with an invert operation reused across cycles", t, func() {
		op := &invertOp{}
		stream := NewStream(WithOperations(op))

		input := []byte("cycle")
		expected := make([]byte, len(input))

		for idx, byt := range input {
			expected[idx] = byt ^ 0xFF
		}

		for round := 0; round < 3; round++ {
			Convey(fmt.Sprintf("It should transform on cycle %d", round), func() {
				out := new(bytes.Buffer)
				readDone := make(chan error, 1)

				go func() {
					_, readErr := io.Copy(out, stream)
					readDone <- readErr
				}()

				written, writeErr := io.Copy(stream, bytes.NewReader(input))
				So(writeErr, ShouldBeNil)
				So(written, ShouldEqual, int64(len(input)))

				So(stream.Close(), ShouldBeNil)
				So(<-readDone, ShouldBeNil)
				So(bytes.Equal(out.Bytes(), expected), ShouldBeTrue)
			})
		}
	})
}

func benchmarkRoundTrip(b *testing.B, payload []byte) {
	b.Helper()
	b.ReportAllocs()

	stream := NewStream()

	var copyBuf [benchmarkCopyBufSize]byte
	copyScratch := copyBuf[:]

	var payloadReader bytes.Reader
	var out bytes.Buffer

	if len(payload) > 0 {
		out.Grow(len(payload))
	}

	readErr := make(chan error, 1)
	readKick := make(chan struct{})

	go func() {
		for range readKick {
			_, err := io.CopyBuffer(&out, stream, copyScratch)
			readErr <- err
		}
	}()

	b.Cleanup(func() {
		close(readKick)
	})

	for b.Loop() {
		out.Reset()
		payloadReader.Reset(payload)

		readKick <- struct{}{}

		if _, err := io.CopyBuffer(stream, &payloadReader, copyScratch); err != nil {
			b.Fatal(err)
		}

		if err := stream.Close(); err != nil {
			b.Fatal(err)
		}

		if err := <-readErr; err != nil {
			b.Fatal(err)
		}

		if !bytes.Equal(out.Bytes(), payload) {
			b.Fatalf("round-trip mismatch len=%d", len(payload))
		}
	}
}

func BenchmarkBackendRoundTrip(b *testing.B) {
	lengths := []int{0, 1, 64, 4095, 4096, 65536, 1 << 20}
	seedRng := rand.New(rand.NewSource(42))

	for _, length := range lengths {
		var payload []byte

		if length > 0 {
			payload = make([]byte, length)

			for index := range payload {
				payload[index] = byte(seedRng.Uint32())
			}
		}

		b.Run(fmt.Sprintf("bytes_%d", length), func(b *testing.B) {
			benchmarkRoundTrip(b, payload)
		})
	}
}

func BenchmarkBackendRoundTripParallel(b *testing.B) {
	const length = 4096

	payload := make([]byte, length)
	seedRng := rand.New(rand.NewSource(43))

	for index := range payload {
		payload[index] = byte(seedRng.Uint32())
	}

	b.ReportAllocs()
	b.SetParallelism(min(8, runtime.GOMAXPROCS(0)))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		stream := NewStream()

		defer func() {
			_ = stream.Close()
		}()

		var copyBuf [benchmarkCopyBufSize]byte
		copyScratch := copyBuf[:]

		var payloadReader bytes.Reader
		var out bytes.Buffer

		out.Grow(len(payload))

		readErr := make(chan error, 1)
		readKick := make(chan struct{})

		go func() {
			for range readKick {
				_, err := io.CopyBuffer(&out, stream, copyScratch)
				readErr <- err
			}
		}()

		defer close(readKick)

		for pb.Next() {
			out.Reset()

			payloadReader.Reset(payload)

			readKick <- struct{}{}

			if _, err := io.CopyBuffer(stream, &payloadReader, copyScratch); err != nil {
				b.Fatal(err)
			}

			if err := stream.Close(); err != nil {
				b.Fatal(err)
			}

			if err := <-readErr; err != nil {
				b.Fatal(err)
			}
		}
	})
}

package kernel

import (
	"context"
	"strings"
	"testing"
	"time"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
)

func TestWorkRequestMessageRoundTrip(t *testing.T) {
	Convey("Given a valid distributed work request payload", t, func() {
		dictionary := []byte{
			0x01, 0x02, 0x03, 0x04,
			0x05, 0x06, 0x07, 0x08,
		}
		context := []byte{0x11, 0x12, 0x13, 0x14}
		numNodes := 2

		Convey("When building and parsing a request message", func() {
			msg, err := newWorkRequestMessage(dictionary, numNodes, context)
			So(err, ShouldBeNil)

			parsedDict, parsedNodes, parsedContext, err := parseWorkRequestMessage(msg)
			So(err, ShouldBeNil)

			Convey("It should preserve all payload fields", func() {
				So(parsedNodes, ShouldEqual, numNodes)
				So(parsedDict, ShouldResemble, dictionary)
				So(parsedContext, ShouldResemble, context)
			})
		})
	})
}

func TestWorkRequestMessageRejectsWrongType(t *testing.T) {
	Convey("Given a response message", t, func() {
		msg, err := newWorkResponseMessage(42, messageErrNone)
		So(err, ShouldBeNil)

		Convey("When parsed as a work request", func() {
			_, _, _, parseErr := parseWorkRequestMessage(msg)

			Convey("It should return an error", func() {
				So(parseErr, ShouldNotBeNil)
			})
		})
	})
}

func TestWorkResponseMessageRoundTrip(t *testing.T) {
	Convey("Given a valid distributed work response payload", t, func() {
		packed := uint64(1337)
		code := uint32(messageErrCompute)

		Convey("When building and parsing a response message", func() {
			msg, err := newWorkResponseMessage(packed, code)
			So(err, ShouldBeNil)

			parsedPacked, parsedCode, err := parseWorkResponseMessage(msg)
			So(err, ShouldBeNil)

			Convey("It should preserve result and error code", func() {
				So(parsedPacked, ShouldEqual, packed)
				So(parsedCode, ShouldEqual, code)
			})
		})
	})
}

func TestDistributedTimeout(t *testing.T) {
	Convey("Given distributed timeout settings", t, func() {
		originalTimeout := config.System.Timeout
		Reset(func() {
			config.System.Timeout = originalTimeout
		})

		Convey("When timeout is non-positive", func() {
			config.System.Timeout = 0
			So(distributedTimeout(), ShouldEqual, 5*time.Second)
		})

		Convey("When timeout is configured", func() {
			config.System.Timeout = 1750
			So(distributedTimeout(), ShouldEqual, 1750*time.Millisecond)
		})
	})
}

func TestDistributedResolveRequiresWorkers(t *testing.T) {
	Convey("Given a distributed backend with no configured workers", t, func() {
		originalWorkers := append([]string(nil), config.System.Workers...)
		originalTimeout := config.System.Timeout
		Reset(func() {
			config.System.Workers = originalWorkers
			config.System.Timeout = originalTimeout
		})

		config.System.Workers = nil
		config.System.Timeout = 1000

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 2, pool.NewConfig())
		defer workerPool.Close()

		backend, err := NewDistributedBackend(
			DistributedWithContext(ctx),
			DistributedWithPool(workerPool),
		)
		So(err, ShouldBeNil)

		graphNodes := []byte{0x01, 0x02, 0x03, 0x04}
		contextBytes := []byte{0x11, 0x12, 0x13, 0x14}

		Convey("Resolve should fail fast with a clear error", func() {
			_, resolveErr := backend.Resolve(
				unsafe.Pointer(&graphNodes[0]),
				1,
				unsafe.Pointer(&contextBytes[0]),
			)
			So(resolveErr, ShouldNotBeNil)
			So(resolveErr.Error(), ShouldContainSubstring, "no workers available")
		})
	})
}

func TestDistributedResolveReturnsRemoteFailure(t *testing.T) {
	Convey("Given a distributed backend with an unreachable worker", t, func() {
		originalWorkers := append([]string(nil), config.System.Workers...)
		originalTimeout := config.System.Timeout
		Reset(func() {
			config.System.Workers = originalWorkers
			config.System.Timeout = originalTimeout
		})

		config.System.Workers = []string{"127.0.0.1:1"}
		config.System.Timeout = 100

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		workerPool := pool.New(ctx, 1, 2, pool.NewConfig())
		defer workerPool.Close()

		backend, err := NewDistributedBackend(
			DistributedWithContext(ctx),
			DistributedWithPool(workerPool),
		)
		So(err, ShouldBeNil)

		graphNodes := []byte{0x01, 0x02, 0x03, 0x04}
		contextBytes := []byte{0x11, 0x12, 0x13, 0x14}

		Convey("Resolve should return the remote scheduling failure", func() {
			_, resolveErr := backend.Resolve(
				unsafe.Pointer(&graphNodes[0]),
				1,
				unsafe.Pointer(&contextBytes[0]),
			)
			So(resolveErr, ShouldNotBeNil)
			message := resolveErr.Error()
			So(
				strings.Contains(message, "distributed chunk") ||
					strings.Contains(message, "context deadline exceeded"),
				ShouldBeTrue,
			)
		})
	})
}

func BenchmarkWorkRequestRoundTrip(b *testing.B) {
	dictionary := make([]byte, 4*256)
	for i := range dictionary {
		dictionary[i] = byte(i)
	}
	contextBytes := []byte{0x01, 0x02, 0x03, 0x04}

	b.ReportAllocs()
	for b.Loop() {
		msg, err := newWorkRequestMessage(dictionary, 256, contextBytes)
		if err != nil {
			b.Fatalf("newWorkRequestMessage failed: %v", err)
		}

		_, _, _, err = parseWorkRequestMessage(msg)
		if err != nil {
			b.Fatalf("parseWorkRequestMessage failed: %v", err)
		}
	}
}

func BenchmarkWorkResponseRoundTrip(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		msg, err := newWorkResponseMessage(1234, messageErrNone)
		if err != nil {
			b.Fatalf("newWorkResponseMessage failed: %v", err)
		}

		_, _, err = parseWorkResponseMessage(msg)
		if err != nil {
			b.Fatalf("parseWorkResponseMessage failed: %v", err)
		}
	}
}

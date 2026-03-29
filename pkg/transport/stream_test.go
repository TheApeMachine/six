package transport

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestMain(m *testing.M) {
	viper.SetConfigFile("../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "...: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "...: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

func TestNewStreamDefaultsToOneRegion(t *testing.T) {
	Convey("Given a Stream with no explicit region option", t, func() {
		stream := NewStream(t.Context())
		defer stream.Close()

		So(stream, ShouldNotBeNil)
		So(stream.regions, ShouldEqual, 1)
		So(len(stream.emitter), ShouldEqual, 1)
	})
}

func TestRead(t *testing.T) {
	Convey("Given a Stream", t, func() {
		regions := 2

		stream := NewStream(
			t.Context(),
			StreamWithTTL(time.Second),
			StreamWithRegions(regions),
		)

		So(stream, ShouldNotBeNil)

		Convey("And a Value is written to the stream", func() {
			for range regions {
				value, err := primitive.NewValue(nil)
				So(err, ShouldBeNil)

				(*value)[core.Cfg.StateIndex] = 1
				n, cerr := io.Copy(stream, value)
				value.Close()

				So(n, ShouldEqual, primitive.ByteSize)
				So(cerr, ShouldBeNil)
			}

			Convey("Then the Value should be read from the stream", func() {
				buf := make([]byte, primitive.ByteSize*regions)
				n, err := io.ReadFull(stream, buf)

				So(n, ShouldEqual, len(buf))
				So(err, ShouldBeNil)
			})
		})
	})
}

func TestWrite(t *testing.T) {
	Convey("Given a Stream", t, func() {
		regions := 2

		stream := NewStream(
			t.Context(),
			StreamWithTTL(time.Second),
			StreamWithRegions(regions),
		)

		So(stream, ShouldNotBeNil)

		Convey("And a Value is written to the stream", func() {
			for range regions {
				value, err := primitive.NewValue(nil)
				So(err, ShouldBeNil)

				(*value)[core.Cfg.StateIndex] = 1
				n, cerr := io.Copy(stream, value)
				value.Close()

				So(n, ShouldEqual, primitive.ByteSize)
				So(cerr, ShouldBeNil)
			}
		})

		Convey("Then the Value should be read from the stream", func() {
			str := []byte("Hello!")
			var decoded strings.Builder

			for range regions {
				value, err := primitive.NewValue(str)
				So(err, ShouldBeNil)

				(*value)[core.Cfg.StateIndex] = 1
				n, cerr := io.Copy(stream, value)
				value.Close()

				So(n, ShouldEqual, primitive.ByteSize)
				So(cerr, ShouldBeNil)
			}

			buf := make([]byte, primitive.ByteSize*regions)
			n, err := io.ReadFull(stream, buf)

			So(n, ShouldEqual, len(buf))
			So(err, ShouldBeNil)

			for i := 0; i < regions; i++ {
				start := i * primitive.ByteSize
				end := start + primitive.ByteSize
				value := primitive.BytesToValue(buf[start:end])
				decoded.WriteString(value.String())
				_ = value.Close()
			}

			So(decoded.String(), ShouldEqual, string(append(str, str...)))
		})
	})
}

func TestReadRotatesAcrossResidentValues(t *testing.T) {
	Convey("Given a one-region Stream with two inert Values", t, func() {
		stream := NewStream(t.Context(), StreamWithRegions(1))
		defer stream.Close()

		mk := func(token string) *primitive.Value {
			value, err := primitive.NewValue([]byte(token))
			So(err, ShouldBeNil)
			for i := core.Cfg.ProgramIndex; i < primitive.Words; i++ {
				(*value)[i] = 0
			}
			(*value)[core.Cfg.FW] = 0
			(*value)[core.Cfg.RegPC] = 0
			(*value)[core.Cfg.StateIndex] = 1
			return value
		}

		valueA := mk("A")
		defer valueA.Close()
		valueB := mk("B")
		defer valueB.Close()

		_, err := io.Copy(stream, valueA)
		So(err, ShouldBeNil)
		_, err = io.Copy(stream, valueB)
		So(err, ShouldBeNil)

		buf := make([]byte, primitive.ByteSize)
		_, err = io.ReadFull(stream, buf)
		So(err, ShouldBeNil)
		first := primitive.BytesToValue(buf)
		So(first.String(), ShouldEqual, "A")
		So(first.Close(), ShouldBeNil)

		_, err = io.ReadFull(stream, buf)
		So(err, ShouldBeNil)
		second := primitive.BytesToValue(buf)
		So(second.String(), ShouldEqual, "B")
		So(second.Close(), ShouldBeNil)
	})
}

func BenchmarkStream(b *testing.B) {
	payload := make([]byte, primitive.ByteSize)
	readBuf := make([]byte, primitive.ByteSize)

	stream := NewStream(context.Background())
	defer stream.Close()

	b.SetBytes(int64(primitive.ByteSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := stream.Write(payload); err != nil {
			b.Fatal(err)
		}

		if _, err := stream.Read(readBuf); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
}

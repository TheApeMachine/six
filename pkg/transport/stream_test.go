package transport

import (
	"bytes"
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

func TestRead(t *testing.T) {
	Convey("Given a Stream", t, func() {
		regions := 2

		stream := NewStream(
			StreamWithContext(t.Context()),
			StreamWithTTL(time.Second),
			StreamWithRegions(regions),
		)

		So(stream, ShouldNotBeNil)

		Convey("And a Value is written to the stream", func() {
			for range regions {
				value, err := primitive.NewValue(nil)
				defer value.Close()

				So(err, ShouldBeNil)

				(*value)[core.Cfg.StateIndex] = 1
				n, err := io.Copy(stream, value)

				So(n, ShouldEqual, primitive.ByteSize)
				So(err, ShouldBeNil)
			}

			Convey("Then the Value should be read from the stream", func() {
				buf := bytes.NewBuffer(make([]byte, 0, primitive.ByteSize*regions))
				n, err := io.Copy(buf, stream)

				So(n, ShouldEqual, primitive.ByteSize*regions)
				So(err, ShouldBeNil)
			})
		})
	})
}

func TestWrite(t *testing.T) {
	Convey("Given a Stream", t, func() {
		regions := 2

		stream := NewStream(
			StreamWithContext(t.Context()),
			StreamWithTTL(time.Second),
			StreamWithRegions(regions),
		)

		So(stream, ShouldNotBeNil)

		Convey("And a Value is written to the stream", func() {
			for range regions {
				value, err := primitive.NewValue(nil)
				defer value.Close()

				So(err, ShouldBeNil)

				(*value)[core.Cfg.StateIndex] = 1
				n, err := io.Copy(stream, value)

				So(n, ShouldEqual, primitive.ByteSize)
				So(err, ShouldBeNil)
			}
		})

		Convey("Then the Value should be read from the stream", func() {
			str := []byte("Hello!")
			var decoded strings.Builder

			for range regions {
				value, err := primitive.NewValue(str)
				defer value.Close()

				(*value)[core.Cfg.StateIndex] = 1
				n, err := io.Copy(stream, value)

				So(n, ShouldEqual, primitive.ByteSize)
				So(err, ShouldBeNil)
			}

			buf := bytes.NewBuffer(make([]byte, 0, primitive.ByteSize*regions))
			n, err := io.Copy(buf, stream)

			So(n, ShouldEqual, primitive.ByteSize*regions)
			So(err, ShouldBeNil)

			for i := 0; i < regions; i++ {
				start := i * primitive.ByteSize
				end := start + primitive.ByteSize
				decoded.WriteString(primitive.BytesToValue(buf.Bytes()[start:end]).String())
			}

			So(decoded.String(), ShouldEqual, string(append(str, str...)))
		})
	})
}

func BenchmarkStream(b *testing.B) {
	payload := make([]byte, primitive.ByteSize)
	readBuf := make([]byte, primitive.ByteSize)

	stream := NewStream(StreamWithContext(context.Background()))
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

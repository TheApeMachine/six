package transport

// import (
// 	"context"
// 	"fmt"
// 	"io"
// 	"math/bits"
// 	"os"
// 	"strings"
// 	"testing"
// 	"time"

// 	. "github.com/smartystreets/goconvey/convey"
// 	"github.com/spf13/viper"
// 	"github.com/theapemachine/six/pkg/core"
// 	"github.com/theapemachine/six/pkg/primitive"
// 	"github.com/theapemachine/six/pkg/store"
// )

// func TestMain(m *testing.M) {
// 	viper.SetConfigFile("../../cmd/cfg/config.yml")
// 	if err := viper.ReadInConfig(); err != nil {
// 		fmt.Fprintf(os.Stderr, "...: %v\n", err)
// 		os.Exit(1)
// 	}
// 	if err := core.LoadValueConfig(); err != nil {
// 		fmt.Fprintf(os.Stderr, "...: %v\n", err)
// 		os.Exit(1)
// 	}
// 	code := m.Run()
// 	os.Exit(code)
// }

// func TestNewStreamDefaultsToOneRegion(t *testing.T) {
// 	Convey("Given a Stream with no explicit region option", t, func() {
// 		stream := NewStream(t.Context())
// 		defer stream.Close()

// 		So(stream, ShouldNotBeNil)
// 		So(stream.regions, ShouldEqual, 1)
// 		So(len(stream.emitter), ShouldEqual, 1)
// 	})
// }

// func TestRead(t *testing.T) {
// 	Convey("Given a Stream", t, func() {
// 		regions := 2

// 		stream := NewStream(
// 			t.Context(),
// 			StreamWithTTL(time.Second),
// 			StreamWithRegions(regions),
// 		)

// 		So(stream, ShouldNotBeNil)

// 		Convey("And a Value is written to the stream", func() {
// 			for idx := range regions {
// 				value, err := primitive.NewValue(nil)
// 				So(err, ShouldBeNil)
// 				(*value)[core.Cfg.AffinityIndex] = affinityForRegion(idx, regions)

// 				(*value)[core.Cfg.StateIndex] = 1
// 				n, cerr := io.Copy(stream, value)
// 				value.Close()

// 				So(n, ShouldEqual, primitive.ByteSize)
// 				So(cerr, ShouldBeNil)
// 			}

// 			Convey("Then the Value should be read from the stream", func() {
// 				buf := make([]byte, primitive.ByteSize)
// 				n, err := io.ReadFull(stream, buf)
// 				So(n, ShouldEqual, len(buf))
// 				So(err, ShouldBeNil)

// 				n, err = io.ReadFull(stream, buf)
// 				So(n, ShouldEqual, len(buf))
// 				So(err, ShouldBeNil)
// 			})
// 		})
// 	})
// }

// func TestWrite(t *testing.T) {
// 	Convey("Given a Stream", t, func() {
// 		regions := 2

// 		stream := NewStream(
// 			t.Context(),
// 			StreamWithTTL(time.Second),
// 			StreamWithRegions(regions),
// 		)

// 		So(stream, ShouldNotBeNil)

// 		Convey("And a Value is written to the stream", func() {
// 			for range regions {
// 				value, err := primitive.NewValue(nil)
// 				So(err, ShouldBeNil)

// 				(*value)[core.Cfg.StateIndex] = 1
// 				n, cerr := io.Copy(stream, value)
// 				value.Close()

// 				So(n, ShouldEqual, primitive.ByteSize)
// 				So(cerr, ShouldBeNil)
// 			}
// 		})

// 		Convey("Then the Value should be read from the stream", func() {
// 			str := []byte("Hello!")
// 			var decoded strings.Builder

// 			for idx := range regions {
// 				value, err := primitive.NewValue(str)
// 				So(err, ShouldBeNil)
// 				mockAffinity := affinityForRegion(idx, regions)
// 				(*value)[core.Cfg.AffinityIndex] = mockAffinity

// 				// Re-insert into LSM under the mock affinity so String() works
// 				var tokens []uint64
// 				var affs []uint64
// 				for i, ch := range str {
// 					tokens = append(tokens, primitive.Tokenize(ch, uint64(i)))
// 					affs = append(affs, mockAffinity)
// 				}
// 				store.DefaultSpatialIndex().InsertBatch(tokens, affs)

// 				(*value)[core.Cfg.StateIndex] = 1
// 				n, cerr := io.Copy(stream, value)
// 				value.Close()

// 				So(n, ShouldEqual, primitive.ByteSize)
// 				So(cerr, ShouldBeNil)
// 			}

// 			for range regions {
// 				buf := make([]byte, primitive.ByteSize)
// 				n, err := io.ReadFull(stream, buf)
// 				So(n, ShouldEqual, len(buf))
// 				So(err, ShouldBeNil)

// 				value := primitive.BytesToValue(buf)
// 				decoded.WriteString(value.String())
// 				_ = value.Close()
// 			}

// 			So(decoded.String(), ShouldEqual, string(append(str, str...)))
// 		})
// 	})
// }

// func TestReadRotatesAcrossResidentValues(t *testing.T) {
// 	Convey("Given a one-region Stream with two inert Values", t, func() {
// 		stream := NewStream(t.Context(), StreamWithRegions(1))
// 		defer stream.Close()

// 		mk := func(token string) *primitive.Value {
// 			value, err := primitive.NewValue([]byte(token))
// 			So(err, ShouldBeNil)
// 			for i := core.Cfg.ProgramIndex; i < primitive.Words; i++ {
// 				(*value)[i] = 0
// 			}
// 			(*value)[core.Cfg.FW] = 0
// 			(*value)[core.Cfg.RegPC] = 0
// 			(*value)[core.Cfg.StateIndex] = 1
// 			return value
// 		}

// 		valueA := mk("A")
// 		defer valueA.Close()
// 		valueB := mk("B")
// 		defer valueB.Close()

// 		_, err := io.Copy(stream, valueA)
// 		So(err, ShouldBeNil)
// 		_, err = io.Copy(stream, valueB)
// 		So(err, ShouldBeNil)

// 		buf := make([]byte, primitive.ByteSize)
// 		_, err = io.ReadFull(stream, buf)
// 		So(err, ShouldBeNil)
// 		first := primitive.BytesToValue(buf)
// 		So(first.String(), ShouldEqual, "A")
// 		So(first.Close(), ShouldBeNil)

// 		_, err = io.ReadFull(stream, buf)
// 		So(err, ShouldBeNil)
// 		second := primitive.BytesToValue(buf)
// 		So(second.String(), ShouldEqual, "B")
// 		So(second.Close(), ShouldBeNil)
// 	})
// }

// func TestWriteRoutesByAffinityBits(t *testing.T) {
// 	Convey("Given a multi-region Stream", t, func() {
// 		stream := NewStream(t.Context(), StreamWithRegions(4))
// 		defer stream.Close()

// 		mk := func(token string, region int) *primitive.Value {
// 			value, err := primitive.NewValue([]byte(token))
// 			So(err, ShouldBeNil)
// 			(*value)[core.Cfg.AffinityIndex] = affinityForRegion(region, 4)
// 			(*value)[core.Cfg.StateIndex] = 1
// 			return value
// 		}

// 		left := mk("A", 1)
// 		defer left.Close()
// 		right := mk("B", 3)
// 		defer right.Close()

// 		_, err := io.Copy(stream, left)
// 		So(err, ShouldBeNil)
// 		_, err = io.Copy(stream, right)
// 		So(err, ShouldBeNil)

// 		So(stream.pending[0], ShouldEqual, 0)
// 		So(stream.pending[1], ShouldEqual, 1)
// 		So(stream.pending[2], ShouldEqual, 0)
// 		So(stream.pending[3], ShouldEqual, 1)
// 	})
// }

// func affinityForRegion(region, regions int) uint64 {
// 	if regions <= 1 {
// 		return 0
// 	}
// 	routeBits := bits.Len(uint(regions - 1))
// 	if routeBits <= 0 {
// 		return 0
// 	}
// 	shift := 48 - routeBits
// 	if shift < 0 {
// 		shift = 0
// 	}
// 	return uint64(region) << shift
// }

// func BenchmarkStream(b *testing.B) {
// 	payload := make([]byte, primitive.ByteSize)
// 	readBuf := make([]byte, primitive.ByteSize)

// 	stream := NewStream(context.Background())
// 	defer stream.Close()

// 	b.SetBytes(int64(primitive.ByteSize))
// 	b.ResetTimer()

// 	for i := 0; i < b.N; i++ {
// 		if _, err := stream.Write(payload); err != nil {
// 			b.Fatal(err)
// 		}

// 		if _, err := stream.Read(readBuf); err != nil {
// 			b.Fatal(err)
// 		}
// 	}

// 	b.StopTimer()
// }

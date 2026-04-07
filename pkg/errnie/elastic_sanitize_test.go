package errnie

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSanitizeLogLineForElasticsearch(t *testing.T) {
	t.Parallel()

	Convey("Given JSON log lines destined for Elasticsearch", t, func() {
		Convey("It should leave small integers and normal structure intact", func() {
			in := []byte(`{"n":42}`)
			out := sanitizeLogLineForElasticsearch(in)
			So(string(out), ShouldEqual, `{"n":42}`)
		})

		/*
			dst is renamed to dst_words (ES-safe keyword) holding JSON text;
			uints above MaxInt64 stay as quoted decimals inside that JSON.
		*/
		Convey("It should move dst to dst_words with huge integers as decimals inside JSON text", func() {
			huge := strconv.FormatUint(math.MaxUint64, 10)
			in := []byte(`{"dst":[1,` + huge + `]}`)
			out := sanitizeLogLineForElasticsearch(in)
			var decoded map[string]interface{}
			So(json.Unmarshal(out, &decoded), ShouldBeNil)
			So(decoded["dst"], ShouldBeNil)
			words, ok := decoded["dst_words"].(string)
			So(ok, ShouldBeTrue)
			var inner []interface{}
			So(json.Unmarshal([]byte(words), &inner), ShouldBeNil)
			So(inner[0], ShouldEqual, float64(1))
			So(inner[1], ShouldEqual, huge)
		})

		Convey("It should match ES failure cases from ALU traces (10969528695890054980)", func() {
			in := []byte(`{"msg":"cpu.Backend.handleAlu","dst":[0,8089718375828766777,10969528695890054980],"src":[0,0]}`)
			out := sanitizeLogLineForElasticsearch(in)
			var decoded map[string]interface{}
			So(json.Unmarshal(out, &decoded), ShouldBeNil)
			So(decoded["dst"], ShouldBeNil)
			So(decoded["src"], ShouldBeNil)
			_, hasWords := decoded["dst_words"].(string)
			_, hasSrc := decoded["src_words"].(string)
			So(hasWords, ShouldBeTrue)
			So(hasSrc, ShouldBeTrue)
		})

		Convey("It should rewrite key to key_u64 string for cluster routing uint64", func() {
			huge := strconv.FormatUint(math.MaxUint64, 10)
			in := []byte(`{"msg":"cluster.kademlia.Insert","key":` + huge + `}`)
			out := sanitizeLogLineForElasticsearch(in)
			var decoded map[string]interface{}
			So(json.Unmarshal(out, &decoded), ShouldBeNil)
			So(decoded["key"], ShouldBeNil)
			s, ok := decoded["key_u64"].(string)
			So(ok, ShouldBeTrue)
			So(s, ShouldEqual, huge)
		})

		Convey("It should force correlation_id to decimal string for huge uint64", func() {
			huge := strconv.FormatUint(math.MaxUint64, 10)
			in := []byte(`{"correlation_id":` + huge + `}`)
			out := sanitizeLogLineForElasticsearch(in)
			var decoded map[string]interface{}
			So(json.Unmarshal(out, &decoded), ShouldBeNil)
			s, ok := decoded["correlation_id"].(string)
			So(ok, ShouldBeTrue)
			So(s, ShouldEqual, huge)
		})

		Convey("It should pass through invalid JSON unchanged", func() {
			in := []byte(`not-json`)
			out := sanitizeLogLineForElasticsearch(in)
			So(out, ShouldResemble, in)
		})
	})
}

func TestNormalizeJSONNumber(t *testing.T) {
	t.Parallel()

	Convey("Given json.Number values", t, func() {
		Convey("It should keep signed int64 range as numbers", func() {
			So(normalizeJSONNumber(json.Number("42")), ShouldEqual, int64(42))
		})

		Convey("It should return strings for unsigned overflow of long", func() {
			huge := strconv.FormatUint(math.MaxUint64, 10)
			So(normalizeJSONNumber(json.Number(huge)), ShouldEqual, huge)
		})
	})
}

func BenchmarkSanitizeLogLineForElasticsearch(b *testing.B) {
	line := []byte(`{"op":0,"dst":[1,8089718375828766777,18446744073709551615],"src":[0,0]}`)
	b.ResetTimer()
	for range b.N {
		sanitizeLogLineForElasticsearch(line)
	}
}

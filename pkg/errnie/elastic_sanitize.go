package errnie

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

/*
sanitizeLogLineForElasticsearch rewrites one JSON log line so numeric fields stay
compatible with Elasticsearch dynamic mappings.

Zap encodes []uint64 (e.g. ALU dst/src words) as JSON numbers. Values above
math.MaxInt64 cannot be stored in ES "long" (signed 64-bit). Older indices often
already map dst/src as long, so we:

  - rewrite dst → dst_words and src → src_words
  - set those fields to a single JSON text string of the sanitized array/object
  - rewrite key → key_u64 as a decimal string so routing/affinity uint64 values
    (and existing indices that already mapped key as signed long) do not reject
    values above math.MaxInt64.

New fields pick up keyword/text mapping; the oversized long problem and
mixed long/string arrays go away without deleting the index.

If unmarshaling fails, the original line is returned unchanged.
*/
func sanitizeLogLineForElasticsearch(line []byte) []byte {

	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()

	var root interface{}

	if err := dec.Decode(&root); err != nil {
		return line
	}

	root = sanitizeJSONValue(root)

	out, err := json.Marshal(root)
	if err != nil {
		return line
	}

	return out
}

func sanitizeJSONValue(value interface{}) interface{} {

	switch typed := value.(type) {
	case json.Number:
		return normalizeJSONNumber(typed)
	case map[string]interface{}:
		for key, inner := range typed {
			if key == "dst" || key == "src" {
				rename := "dst_words"
				if key == "src" {
					rename = "src_words"
				}
				typed[rename] = stringifyForElasticsearchTraceWords(sanitizeJSONValue(inner))
				delete(typed, key)
				continue
			}

			if key == "key" {
				typed["key_u64"] = stringifyForElasticsearchNumericKey(sanitizeJSONValue(inner))
				delete(typed, key)
				continue
			}

			if key == "correlation_id" {
				typed["correlation_id"] = stringifyForElasticsearchNumericKey(sanitizeJSONValue(inner))
				continue
			}

			typed[key] = sanitizeJSONValue(inner)
		}
		return typed
	case []interface{}:
		for index := range typed {
			typed[index] = sanitizeJSONValue(typed[index])
		}
		return typed
	case float64:
		return normalizeFloat64ForElasticsearch(typed)
	default:
		return value
	}
}

func stringifyForElasticsearchTraceWords(value interface{}) string {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(b)
}

/*
stringifyForElasticsearchNumericKey turns trace numeric keys into a single
decimal string so dynamic mapping prefers keyword and clusters never coerce
uint64-sized values into a signed long.
*/
func stringifyForElasticsearchNumericKey(value interface{}) string {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed != math.Trunc(typed) {
			return strconv.FormatFloat(typed, 'f', -1, 64)
		}

		if typed >= float64(math.MinInt64) && typed <= float64(math.MaxInt64) {
			return strconv.FormatInt(int64(typed), 10)
		}

		return strconv.FormatFloat(typed, 'f', 0, 64)
	case string:
		return typed
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}

		return string(b)
	}
}

func normalizeFloat64ForElasticsearch(value float64) interface{} {
	if value != value {
		return "NaN"
	}
	if value != math.Trunc(value) {
		return value
	}
	if value > float64(math.MaxInt64) || value < float64(math.MinInt64) {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	return int64(value)
}

func normalizeJSONNumber(number json.Number) interface{} {

	raw := number.String()

	if strings.ContainsAny(raw, ".eE") {
		flt, err := number.Float64()
		if err != nil {
			return raw
		}
		return flt
	}

	if intVal, err := number.Int64(); err == nil {
		return intVal
	}

	unsigned, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		flt, ferr := number.Float64()
		if ferr != nil {
			return raw
		}
		return flt
	}

	if unsigned > uint64(math.MaxInt64) {
		return raw
	}

	return int64(unsigned)
}


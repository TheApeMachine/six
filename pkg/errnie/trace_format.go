package errnie

import (
	"encoding/hex"
	"fmt"
)

// TraceStringer can be implemented by types that want a compact, human-oriented
// representation when passed to Trace (structured logs and trace files), instead
// of zap's default encoding (e.g. base64 for []byte).
type TraceStringer interface {
	TraceString() string
}

const (
	// TraceByteHexPrefixLen is how many leading bytes of a []byte are hex-encoded in Trace.
	TraceByteHexPrefixLen = 48
	// TraceTokenPreviewRunes caps token text when a type implements TraceStringer (e.g. *primitive.Value).
	TraceTokenPreviewRunes = 120
)

func formatForTrace(v any) any {
	if v == nil {
		return nil
	}
	if t, ok := v.(TraceStringer); ok {
		return t.TraceString()
	}
	switch x := v.(type) {
	case []byte:
		return formatTraceByteSlice(x)
	default:
		return v
	}
}

func formatTraceByteSlice(p []byte) string {
	if len(p) == 0 {
		return "len=0"
	}
	n := len(p)
	prefixLen := TraceByteHexPrefixLen
	if prefixLen > n {
		prefixLen = n
	}
	s := hex.EncodeToString(p[:prefixLen])
	if n > prefixLen {
		return fmt.Sprintf("len=%d hex_prefix=%s…", n, s)
	}
	return fmt.Sprintf("len=%d hex=%s", n, s)
}

// traceKeyvalsFormatted returns a copy of keyvals with odd-index values passed through formatForTrace.
// Even indices are treated as keys, odd as values (zap-style pairs). An odd-length slice leaves the
// final key unpaired; that value is left unchanged and we log once so callers notice the mismatch.
func traceKeyvalsFormatted(keyvals []any) []any {
	if len(keyvals) == 0 {
		return nil
	}
	out := make([]any, len(keyvals))
	copy(out, keyvals)
	if len(out)%2 == 1 {
		Warn(
			"errnie.traceKeyvalsFormatted",
			"msg", "odd-length keyvals: trailing key has no paired value",
			"trailing_key", out[len(out)-1],
		)
	}
	for i := 1; i < len(out); i += 2 {
		out[i] = formatForTrace(out[i])
	}
	return out
}


package viz

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

/*
Binary WebSocket frames for the viz UI (version 1).

Layout is little-endian. All string/blob lengths are uint32 so trie graph JSON
and other large meta values are not capped at 64KiB.

	[0:4]   magic — 'V','Z','B', version (0x01)
	[4]     frame type — 1=event, 2=bootstrap, 3=stats, 4=scrub, 5=json blob

Event payload (type 1):

	[0:8]   int64 timestamp (unix microseconds)
	[8]     uint8 EventKind
	[9:]    src UTF-8  (u32 len + bytes)
	        tgt UTF-8
	        lbl UTF-8
	        u32 n_vals — pairs (u32 keyLen, key, f64)
	        u32 n_meta — pairs (u32 keyLen, key, u32 valLen, val)

Bootstrap (type 2):

	u32 n — then n times (u32 len, node id utf8)

Stats (type 3):

	u64 dropped

Scrub (type 4):

	u32 n_events — then n times (u32 payloadLen, event payload only — same bytes as
	type-1 payload without the 5-byte outer frame)
*/

const (
	wireMagic0  = 'V'
	wireMagic1  = 'Z'
	wireMagic2  = 'B'
	wireVersion = 1
)

const (
	WireFrameEvent = iota + 1
	WireFrameBootstrap
	WireFrameStats
	WireFrameScrub
	// WireFrameJSONBlob carries a legacy JSON object (e.g. snapshot_data) inside binary transport.
	WireFrameJSONBlob
)

var errWireTrunc = errors.New("viz wire: truncated")
var errWireMagic = errors.New("viz wire: bad magic")

func wireHeader(frameType byte) []byte {
	return []byte{wireMagic0, wireMagic1, wireMagic2, wireVersion, frameType}
}

func appendU32(b []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(b, v)
}

func appendU64(b []byte, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(b, v)
}

func appendI64(b []byte, v int64) []byte {
	return binary.LittleEndian.AppendUint64(b, uint64(v))
}

func appendF64(b []byte, v float64) []byte {
	return binary.LittleEndian.AppendUint64(b, math.Float64bits(v))
}

func appendBlob(b []byte, p []byte) []byte {
	b = appendU32(b, uint32(len(p)))
	b = append(b, p...)
	return b
}

func appendStringU32(b []byte, s string) []byte {
	return appendBlob(b, []byte(s))
}

/*
MarshalWireEvent returns a full binary WebSocket frame for one Event.
*/
func MarshalWireEvent(ev Event) []byte {
	body := marshalEventPayload(ev)
	out := make([]byte, 0, 5+len(body))
	out = append(out, wireHeader(WireFrameEvent)...)
	out = append(out, body...)
	return out
}

func marshalEventPayload(ev Event) []byte {
	b := make([]byte, 0, 64)
	b = appendI64(b, ev.Timestamp)
	b = append(b, byte(ev.Kind))
	b = appendStringU32(b, ev.Source)
	b = appendStringU32(b, ev.Target)
	b = appendStringU32(b, ev.Label)

	valuesCount := uint32(len(ev.Values))
	b = appendU32(b, valuesCount)

	if valuesCount > 0 {
		for key, value := range ev.Values {
			b = appendStringU32(b, key)
			b = appendF64(b, value)
		}
	}

	metaCount := uint32(len(ev.Meta))
	b = appendU32(b, metaCount)

	if metaCount > 0 {
		for key, valueStr := range ev.Meta {
			b = appendStringU32(b, key)
			b = appendStringU32(b, valueStr)
		}
	}

	return b
}

/*
MarshalWireBootstrap returns a bootstrap frame listing known node ids.
*/
func MarshalWireBootstrap(nodeIDs []string) []byte {
	b := make([]byte, 0, 5+4+len(nodeIDs)*8)
	b = append(b, wireHeader(WireFrameBootstrap)...)
	b = appendU32(b, uint32(len(nodeIDs)))
	for _, id := range nodeIDs {
		b = appendStringU32(b, id)
	}

	return b
}

/*
MarshalWireStats returns a dropped-counter frame for the HUD.
*/
func MarshalWireStats(dropped uint64) []byte {
	b := make([]byte, 0, 13)
	b = append(b, wireHeader(WireFrameStats)...)
	b = appendU64(b, dropped)
	return b
}

/*
MarshalWireScrub returns a batch of events for timeline scrub replay.
*/
func MarshalWireScrub(events []Event) []byte {
	b := make([]byte, 0, 5+4+len(events)*64)
	b = append(b, wireHeader(WireFrameScrub)...)
	b = appendU32(b, uint32(len(events)))
	for _, ev := range events {
		payload := marshalEventPayload(ev)
		b = appendU32(b, uint32(len(payload)))
		b = append(b, payload...)
	}

	return b
}

/*
MarshalWireJSONBlob wraps an already-encoded JSON document in a binary frame so the
outbound WebSocket path can stay BinaryMessage-only.
*/
func MarshalWireJSONBlob(jsonBytes []byte) []byte {
	b := make([]byte, 0, 5+4+len(jsonBytes))
	b = append(b, wireHeader(WireFrameJSONBlob)...)
	b = appendU32(b, uint32(len(jsonBytes)))
	b = append(b, jsonBytes...)
	return b
}

func readU32(data []byte, i int) (v uint32, next int, err error) {
	if i+4 > len(data) {
		return 0, i, errWireTrunc
	}

	return binary.LittleEndian.Uint32(data[i:]), i + 4, nil
}

func readU64(data []byte, i int) (v uint64, next int, err error) {
	if i+8 > len(data) {
		return 0, i, errWireTrunc
	}

	return binary.LittleEndian.Uint64(data[i:]), i + 8, nil
}

func readI64(data []byte, i int) (v int64, next int, err error) {
	u, n, err := readU64(data, i)
	if err != nil {
		return 0, i, err
	}

	return int64(u), n, nil
}

func readF64(data []byte, i int) (v float64, next int, err error) {
	u, n, err := readU64(data, i)
	if err != nil {
		return 0, i, err
	}

	return math.Float64frombits(u), n, nil
}

func readBlob(data []byte, i int) (blob []byte, next int, err error) {
	ln, j, err := readU32(data, i)
	if err != nil {
		return nil, i, err
	}

	if ln > 1<<28 {
		return nil, i, fmt.Errorf("viz wire: blob length %d excessive", ln)
	}

	end := j + int(ln)
	if end > len(data) {
		return nil, i, errWireTrunc
	}

	return data[j:end], end, nil
}

func readString(data []byte, i int) (s string, next int, err error) {
	blob, n, err := readBlob(data, i)
	if err != nil {
		return "", i, err
	}

	return string(blob), n, nil
}

/*
UnmarshalWireEventPayload parses the event body (without the 5-byte frame header).
*/
func UnmarshalWireEventPayload(data []byte) (Event, error) {
	var ev Event

	i := 0
	ts, n, err := readI64(data, i)
	if err != nil {
		return ev, err
	}

	ev.Timestamp = ts
	i = n
	if i >= len(data) {
		return ev, errWireTrunc
	}

	ev.Kind = EventKind(data[i])
	ev.Values = make(map[string]float64)
	ev.Meta = make(map[string]string)
	i++

	s, n, err := readString(data, i)
	if err != nil {
		return ev, err
	}

	ev.Source = s
	i = n

	s, n, err = readString(data, i)
	if err != nil {
		return ev, err
	}

	ev.Target = s
	i = n

	s, n, err = readString(data, i)
	if err != nil {
		return ev, err
	}

	ev.Label = s
	i = n

	nVals, n, err := readU32(data, i)
	if err != nil {
		return ev, err
	}

	i = n
	for k := uint32(0); k < nVals; k++ {
		keyStr, ni, err := readString(data, i)
		if err != nil {
			return ev, err
		}

		i = ni
		fv, ni, err := readF64(data, i)
		if err != nil {
			return ev, err
		}

		ev.Values[keyStr] = fv
		i = ni
	}

	nMeta, n, err := readU32(data, i)
	if err != nil {
		return ev, err
	}

	i = n
	for k := uint32(0); k < nMeta; k++ {
		keyStr, ni, err := readString(data, i)
		if err != nil {
			return ev, err
		}

		i = ni
		valStr, ni, err := readString(data, i)
		if err != nil {
			return ev, err
		}

		ev.Meta[keyStr] = valStr
		i = ni
	}

	if i != len(data) {
		return ev, fmt.Errorf("viz wire: %d trailing bytes after event", len(data)-i)
	}

	return ev, nil
}

func checkFrameHeader(data []byte) (frameType byte, payloadOff int, err error) {
	if len(data) < 5 {
		return 0, 0, errWireTrunc
	}

	if data[0] != wireMagic0 || data[1] != wireMagic1 || data[2] != wireMagic2 || data[3] != wireVersion {
		return 0, 0, errWireMagic
	}

	return data[4], 5, nil
}

/*
UnmarshalWireMessage parses any supported server→client frame.
*/
func UnmarshalWireMessage(data []byte) (frameType byte, ev Event, nodes []string, dropped uint64, scrub []Event, err error) {
	ft, off, err := checkFrameHeader(data)
	if err != nil {
		return 0, ev, nil, 0, nil, err
	}

	payload := data[off:]

	switch ft {
	case WireFrameEvent:
		ev, err = UnmarshalWireEventPayload(payload)
		if err != nil {
			return 0, ev, nil, 0, nil, err
		}

		return WireFrameEvent, ev, nil, 0, nil, nil

	case WireFrameBootstrap:
		n, i, err := readU32(payload, 0)
		if err != nil {
			return 0, ev, nil, 0, nil, err
		}

		nodes = make([]string, 0, n)
		for k := uint32(0); k < n; k++ {
			s, ni, err := readString(payload, i)
			if err != nil {
				return 0, ev, nil, 0, nil, err
			}

			nodes = append(nodes, s)
			i = ni
		}

		if i != len(payload) {
			return 0, ev, nil, 0, nil, fmt.Errorf("viz wire: bootstrap trailing %d", len(payload)-i)
		}

		return WireFrameBootstrap, ev, nodes, 0, nil, nil

	case WireFrameStats:
		d, i, err := readU64(payload, 0)
		if err != nil {
			return 0, ev, nil, 0, nil, err
		}

		if i != len(payload) {
			return 0, ev, nil, 0, nil, fmt.Errorf("viz wire: stats trailing")
		}

		return WireFrameStats, ev, nil, d, nil, nil

	case WireFrameScrub:
		nEv, i, err := readU32(payload, 0)
		if err != nil {
			return 0, ev, nil, 0, nil, err
		}

		scrub = make([]Event, 0, nEv)
		for k := uint32(0); k < nEv; k++ {
			chunkLen, ni, err := readU32(payload, i)
			if err != nil {
				return 0, ev, nil, 0, nil, err
			}

			i = ni
			end := i + int(chunkLen)
			if end > len(payload) {
				return 0, ev, nil, 0, nil, errWireTrunc
			}

			chunk := payload[i:end]
			evOne, err := UnmarshalWireEventPayload(chunk)
			if err != nil {
				return 0, ev, nil, 0, nil, err
			}

			scrub = append(scrub, evOne)
			i = end
		}

		if i != len(payload) {
			return 0, ev, nil, 0, nil, fmt.Errorf("viz wire: scrub trailing")
		}

		return WireFrameScrub, ev, nil, 0, scrub, nil

	default:
		return 0, ev, nil, 0, nil, fmt.Errorf("viz wire: unknown frame %d", ft)
	}
}

/*
TryUnmarshalWireEvent is used for remote/WebSocket ingress: if data looks like
a viz binary event frame, return the decoded event and true.
*/
func TryUnmarshalWireEvent(data []byte) (Event, bool) {
	ft, off, err := checkFrameHeader(data)
	if err != nil || ft != WireFrameEvent {
		return Event{}, false
	}

	ev, err := UnmarshalWireEventPayload(data[off:])
	if err != nil {
		return Event{}, false
	}

	return ev, true
}

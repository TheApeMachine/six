package telemetry

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

/*
Binary frame layout shared with the browser decoder.
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
	WireFrameJSONBlob
	WireFrameValue
)

var errWireTrunc = errors.New("telemetry wire: truncated")
var errWireMagic = errors.New("telemetry wire: bad magic")

func wireHeader(frameType byte) []byte {
	return []byte{wireMagic0, wireMagic1, wireMagic2, wireVersion, frameType}
}

func appendU32(buffer []byte, value uint32) []byte {
	return binary.LittleEndian.AppendUint32(buffer, value)
}

func appendU64(buffer []byte, value uint64) []byte {
	return binary.LittleEndian.AppendUint64(buffer, value)
}

func appendI64(buffer []byte, value int64) []byte {
	return binary.LittleEndian.AppendUint64(buffer, uint64(value))
}

func appendF64(buffer []byte, value float64) []byte {
	return binary.LittleEndian.AppendUint64(buffer, math.Float64bits(value))
}

func appendBlob(buffer []byte, payload []byte) []byte {
	buffer = appendU32(buffer, uint32(len(payload)))
	buffer = append(buffer, payload...)

	return buffer
}

func appendStringU32(buffer []byte, value string) []byte {
	return appendBlob(buffer, []byte(value))
}

/*
MarshalWireEvent encodes one telemetry event as a VZB frame.
*/
func MarshalWireEvent(event Event) (buffer []byte) {
	defer func() {
		if recover() != nil {
			buffer = nil
		}
	}()

	body := marshalEventPayload(event)
	buffer = make([]byte, 0, 5+len(body))
	buffer = append(buffer, wireHeader(WireFrameEvent)...)
	buffer = append(buffer, body...)

	return buffer
}

func marshalEventPayload(event Event) []byte {
	buffer := make([]byte, 0, 64)
	buffer = appendI64(buffer, event.Timestamp)
	buffer = append(buffer, byte(event.Kind))
	buffer = appendStringU32(buffer, event.Source)
	buffer = appendStringU32(buffer, event.Target)
	buffer = appendStringU32(buffer, event.Label)
	buffer = appendU32(buffer, uint32(len(event.Values)))

	for key, value := range event.Values {
		buffer = appendStringU32(buffer, key)
		buffer = appendF64(buffer, value)
	}

	buffer = appendU32(buffer, uint32(len(event.Meta)))

	for key, value := range event.Meta {
		buffer = appendStringU32(buffer, key)
		buffer = appendStringU32(buffer, value)
	}

	return buffer
}

/*
MarshalWireStats encodes dropped-event statistics.
*/
func MarshalWireStats(dropped uint64) []byte {
	buffer := make([]byte, 0, 13)
	buffer = append(buffer, wireHeader(WireFrameStats)...)
	buffer = appendU64(buffer, dropped)

	return buffer
}

/*
MarshalWireValueFrame wraps a raw Value image and owning id.
*/
func MarshalWireValueFrame(valueID uint64, frame []byte) []byte {
	buffer := make([]byte, 0, 5+8+4+len(frame))
	buffer = append(buffer, wireHeader(WireFrameValue)...)
	buffer = appendU64(buffer, valueID)
	buffer = appendBlob(buffer, frame)

	return buffer
}

func readU32(data []byte, index int) (value uint32, next int, err error) {
	if index+4 > len(data) {
		return 0, index, errWireTrunc
	}

	return binary.LittleEndian.Uint32(data[index:]), index + 4, nil
}

func readU64(data []byte, index int) (value uint64, next int, err error) {
	if index+8 > len(data) {
		return 0, index, errWireTrunc
	}

	return binary.LittleEndian.Uint64(data[index:]), index + 8, nil
}

func readI64(data []byte, index int) (value int64, next int, err error) {
	uintValue, next, err := readU64(data, index)
	if err != nil {
		return 0, index, err
	}

	return int64(uintValue), next, nil
}

func readF64(data []byte, index int) (value float64, next int, err error) {
	uintValue, next, err := readU64(data, index)
	if err != nil {
		return 0, index, err
	}

	return math.Float64frombits(uintValue), next, nil
}

func readBlob(data []byte, index int) (blob []byte, next int, err error) {
	length, offset, err := readU32(data, index)
	if err != nil {
		return nil, index, err
	}

	if length > 1<<28 {
		return nil, index, fmt.Errorf("telemetry wire: blob length %d excessive", length)
	}

	end := offset + int(length)
	if end > len(data) {
		return nil, index, errWireTrunc
	}

	return data[offset:end], end, nil
}

func readString(data []byte, index int) (value string, next int, err error) {
	blob, next, err := readBlob(data, index)
	if err != nil {
		return "", index, err
	}

	return string(blob), next, nil
}

/*
UnmarshalWireEventPayload parses an event body without the 5-byte header.
*/
func UnmarshalWireEventPayload(data []byte) (Event, error) {
	var event Event

	index := 0
	timestamp, next, err := readI64(data, index)
	if err != nil {
		return event, err
	}

	event.Timestamp = timestamp
	index = next

	if index >= len(data) {
		return event, errWireTrunc
	}

	event.Kind = EventKind(data[index])
	index++
	event.Values = make(map[string]float64)
	event.Meta = make(map[string]string)

	if event.Source, index, err = readString(data, index); err != nil {
		return event, err
	}

	if event.Target, index, err = readString(data, index); err != nil {
		return event, err
	}

	if event.Label, index, err = readString(data, index); err != nil {
		return event, err
	}

	valuesCount, next, err := readU32(data, index)
	if err != nil {
		return event, err
	}

	index = next

	for count := uint32(0); count < valuesCount; count++ {
		key, next, err := readString(data, index)
		if err != nil {
			return event, err
		}

		index = next
		value, next, err := readF64(data, index)
		if err != nil {
			return event, err
		}

		index = next
		event.Values[key] = value
	}

	metaCount, next, err := readU32(data, index)
	if err != nil {
		return event, err
	}

	index = next

	for count := uint32(0); count < metaCount; count++ {
		key, next, err := readString(data, index)
		if err != nil {
			return event, err
		}

		index = next
		value, next, err := readString(data, index)
		if err != nil {
			return event, err
		}

		index = next
		event.Meta[key] = value
	}

	return event, nil
}

func checkFrameHeader(data []byte) (frameType byte, offset int, err error) {
	if len(data) < 5 {
		return 0, 0, errWireTrunc
	}

	if data[0] != wireMagic0 || data[1] != wireMagic1 || data[2] != wireMagic2 || data[3] != wireVersion {
		return 0, 0, errWireMagic
	}

	return data[4], 5, nil
}

/*
UnmarshalWireMessage parses any supported telemetry frame.
*/
func UnmarshalWireMessage(data []byte) (
	frameType byte,
	event Event,
	nodes []string,
	dropped uint64,
	scrub []Event,
	valueWireID uint64,
	valueWire []byte,
	err error,
) {
	frameType, offset, err := checkFrameHeader(data)
	if err != nil {
		return 0, event, nil, 0, nil, 0, nil, err
	}

	payload := data[offset:]

	switch frameType {
	case WireFrameEvent:
		event, err = UnmarshalWireEventPayload(payload)
		if err != nil {
			return 0, event, nil, 0, nil, 0, nil, err
		}

		return WireFrameEvent, event, nil, 0, nil, 0, nil, nil

	case WireFrameStats:
		dropped, offset, err = readU64(payload, 0)
		if err != nil {
			return 0, event, nil, 0, nil, 0, nil, err
		}

		if offset != len(payload) {
			return 0, event, nil, 0, nil, 0, nil, fmt.Errorf("telemetry wire: stats trailing")
		}

		return WireFrameStats, event, nil, dropped, nil, 0, nil, nil

	case WireFrameValue:
		valueWireID, offset, err = readU64(payload, 0)
		if err != nil {
			return 0, event, nil, 0, nil, 0, nil, err
		}

		length, offset, err := readU32(payload, offset)
		if err != nil {
			return 0, event, nil, 0, nil, 0, nil, err
		}

		if offset+int(length) > len(payload) {
			return 0, event, nil, 0, nil, 0, nil, errWireTrunc
		}

		valueWire = make([]byte, length)
		copy(valueWire, payload[offset:offset+int(length)])

		return WireFrameValue, event, nil, 0, nil, valueWireID, valueWire, nil

	default:
		return 0, event, nil, 0, nil, 0, nil, fmt.Errorf("telemetry wire: unknown frame %d", frameType)
	}
}

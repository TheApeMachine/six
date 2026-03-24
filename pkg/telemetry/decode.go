package telemetry

import (
	"encoding/json"
	"fmt"
)

/*
DecodeBinary interprets a UDP payload as JSON telemetry. Any non-JSON
payload becomes a small diagnostic event so the listener never breaks.
*/
func DecodeBinary(buf []byte) Event {
	if len(buf) == 0 {
		return Event{
			Component: "System",
			Action:    "Empty",
			Data:      EventData{Message: "zero-length UDP payload"},
		}
	}

	var ev Event
	if err := json.Unmarshal(buf, &ev); err == nil && ev.Component != "" {
		return ev
	}

	preview := len(buf)
	if preview > 48 {
		preview = 48
	}

	return Event{
		Component: "System",
		Action:    "UDP",
		Data: EventData{
			Message: fmt.Sprintf("%d bytes (not JSON): %q", len(buf), string(buf[:preview])),
		},
	}
}

package telemetry

import (
	"net"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

var udpOnce sync.Once

/*
ConfigureFromConfig activates telemetry publishing and, when configured, forwards
all wire frames to the configured UDP endpoint for the visualizer bridge.
*/
func ConfigureFromConfig() {
	ConfigureUDP(core.Cfg.TelemetryEnabled, core.Cfg.TelemetryEndpoint)
}

/*
ConfigureUDP enables the global bus and optionally installs a UDP frame sink.
*/
func ConfigureUDP(enabled bool, endpoint string) {
	if !enabled {
		return
	}

	DefaultBus.Activate()

	if strings.TrimSpace(endpoint) == "" {
		return
	}

	udpOnce.Do(func() {
		conn, err := net.Dial("udp", endpoint)
		if err != nil {
			errnie.Warn("telemetry.ConfigureUDP: dial failed", "endpoint", endpoint, "err", err)
			return
		}

		channel := DefaultBus.Subscribe(8192, nil)

		go func() {
			for event := range channel {
				if _, err := conn.Write(MarshalWireEvent(event)); err != nil {
					errnie.Warn("telemetry.ConfigureUDP: event write failed", "endpoint", endpoint, "err", err)
				}
			}
		}()

		SetWireValueFrameSink(func(payload []byte) {
			if _, err := conn.Write(payload); err != nil {
				errnie.Warn("telemetry.ConfigureUDP: frame write failed", "endpoint", endpoint, "err", err)
			}
		})

		errnie.Info("telemetry.udp", "endpoint", endpoint)
	})
}

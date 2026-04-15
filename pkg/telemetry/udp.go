package telemetry

import (
	"net"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

var udpOnce sync.Once

const maxUDPPayloadBytes = 65507

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

	if strings.TrimSpace(endpoint) == "" {
		return
	}

	udpOnce.Do(func() {
		conn, err := net.Dial("udp", endpoint)
		if err != nil {
			errnie.Warn("telemetry.ConfigureUDP: dial failed", "endpoint", endpoint, "err", err)
			return
		}
		frameChannel := make(chan []byte, 8192)

		go func() {
			for payload := range frameChannel {
				if len(payload) > maxUDPPayloadBytes {
					continue
				}

				if _, err := conn.Write(payload); err != nil {
					errnie.Warn("telemetry.ConfigureUDP: frame write failed", "endpoint", endpoint, "err", err)
				}
			}
		}()

		SetWireValueFrameSink(func(payload []byte) {
			if len(payload) > maxUDPPayloadBytes {
				return
			}

			copyPayload := append([]byte(nil), payload...)

			select {
			case frameChannel <- copyPayload:
			default:
			}
		})

		errnie.Info("telemetry.udp", "endpoint", endpoint)
	})
}

package experiment

import (
	"io"
	"sync/atomic"
	"time"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

type Telemetry struct {
	Instruction  uint8
	Pressure     int
	TotalDensity int
	OpsPerSec    int64
}

type Observer struct {
	Target  io.ReadWriter
	Metrics chan Telemetry

	// Atomic counters to reduce GC pressure at high throughput
	opsCount     atomic.Int64
	lastInstr    atomic.Uint32
	lastPressure atomic.Int64
	lastDensity  atomic.Int64

	done chan struct{}
}

func NewObserver(target io.ReadWriter) *Observer {
	o := &Observer{
		Target:  target,
		Metrics: make(chan Telemetry, 10), // Small buffer, polled at 60Hz
		done:    make(chan struct{}),
	}

	go o.pollMetrics()
	return o
}

func (o *Observer) pollMetrics() {
	ticker := time.NewTicker(time.Second / 60) // 60 Hz
	defer ticker.Stop()

	var lastOps int64
	lastTime := time.Now()

	for {
		select {
		case <-o.done:
			return
		case t := <-ticker.C:
			currentOps := o.opsCount.Load()
			opsDelta := currentOps - lastOps
			lastOps = currentOps

			timeDelta := t.Sub(lastTime).Seconds()
			lastTime = t

			opsPerSec := int64(0)
			if timeDelta > 0 {
				opsPerSec = int64(float64(opsDelta) / timeDelta)
			}

			// Only send if there's activity
			if opsDelta > 0 {
				select {
				case o.Metrics <- Telemetry{
					Instruction:  uint8(o.lastInstr.Load()),
					Pressure:     int(o.lastPressure.Load()),
					TotalDensity: int(o.lastDensity.Load()),
					OpsPerSec:    opsPerSec,
				}:
				default:
				}
			}
		}
	}
}

func (o *Observer) Close() error {
	close(o.done)
	return nil
}

func (o *Observer) Write(p []byte) (n int, err error) {
	n, err = o.Target.Write(p)
	o.measure(p, n)
	return n, err
}

func (o *Observer) Read(p []byte) (n int, err error) {
	n, err = o.Target.Read(p)
	o.measure(p, n)
	return n, err
}

func (o *Observer) measure(p []byte, n int) {
	if n >= primitive.ByteSize {
		val := primitive.BytesToValue(p)

		// Extract observability metrics directly from the topological frame
		pressure := cpu.Popcount(val, int(core.Cfg.StateSequence), 64)
		instr := telemetry.InstructionFromValue(val)
		density := cpu.Popcount(val, int(core.Cfg.StateAccumulator), 64)

		o.lastInstr.Store(uint32(instr))
		o.lastPressure.Store(int64(pressure))
		o.lastDensity.Store(int64(density))
	}
}

package experiment

import (
	"io"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
)

type Telemetry struct {
	Instruction  uint8
	Pressure     int
	TotalDensity int
}

type Observer struct {
	Target  io.ReadWriter
	Metrics chan Telemetry
}

func NewObserver(target io.ReadWriter) *Observer {
	return &Observer{
		Target:  target,
		Metrics: make(chan Telemetry, 1000), // Buffer to prevent stalling
	}
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
		pressure := cpu.Popcount(val, primitive.StateStart, primitive.StateBits)
		instr := uint8(cpu.ReadRegion(val, cpu.RegionInstruction) & 0xF)
		density := cpu.Popcount(val, 0, primitive.CoreBits)

		select {
		case o.Metrics <- Telemetry{
			Instruction:  instr,
			Pressure:     pressure,
			TotalDensity: density,
		}:
		default:
			// Non-blocking drop if telemetry pipe is backed up.
			// The physics engine must never stall for observability!
		}
	}
}

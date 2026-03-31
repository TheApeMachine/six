package visualizer

import (
	"fmt"
	"runtime"

	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
)

/*
SystemTopology describes the runtime layers that sit around the live machine:
Machine, Stream, Emitter, Backend, Pool, and the hardware substrates beneath it.
*/
type SystemTopology struct {
	Title         string               `json:"title"`
	Subtitle      string               `json:"subtitle"`
	StreamRegions int                  `json:"streamRegions"`
	Core          []SystemTopologyNode `json:"core"`
	Hardware      []SystemTopologyNode `json:"hardware"`
	Links         []SystemTopologyLink `json:"links"`
}

/*
SystemTopologyNode is a single node in the orbit.
*/
type SystemTopologyNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Kind   string `json:"kind"`
	Count  int    `json:"count"`
}

/*
SystemTopologyLink connects two nodes in the orbit.
*/
type SystemTopologyLink struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// defaultStreamRegions is the transport ring slot count shown in the UI; align with
// transport.StreamWithRegions when the vm wires a multi-region stream.
const defaultStreamRegions = 4

/*
BuildSystemTopology snapshots the runtime substrate stack for the browser.
*/
func BuildSystemTopology() SystemTopology {
	poolWorkers := runtime.NumCPU() - 1
	if poolWorkers < 1 {
		poolWorkers = 1
	}

	cudaCount := cuda.Available()
	metalCount := metal.Available()
	cpuCount := 1
	backendCount := cudaCount + metalCount + cpuCount

	core := []SystemTopologyNode{
		{
			ID:     "machine",
			Label:  "Machine",
			Detail: "orchestrator",
			Kind:   "machine",
			Count:  1,
		},
		{
			ID:     "stream",
			Label:  "Stream",
			Detail: "transport",
			Kind:   "stream",
			Count:  1,
		},
		{
			ID:     "emitter",
			Label:  "Emitter",
			Detail: "frame capture",
			Kind:   "emitter",
			Count:  1,
		},
		{
			ID:     "backend",
			Label:  "Backend",
			Detail: fmt.Sprintf("%d substrate%s", backendCount, ""),
			Kind:   "backend",
			Count:  backendCount,
		},
		{
			ID:     "pool",
			Label:  "Pool",
			Detail: fmt.Sprintf("%d worker%s", poolWorkers, ""),
			Kind:   "pool",
			Count:  poolWorkers,
		},
	}

	hardware := []SystemTopologyNode{
		{
			ID:     "cuda",
			Label:  "Cuda",
			Detail: fmt.Sprintf("%d %s", cudaCount, "devices"),
			Kind:   "cuda",
			Count:  cudaCount,
		},
		{
			ID:     "metal",
			Label:  "Metal",
			Detail: fmt.Sprintf("%d %s", metalCount, "devices"),
			Kind:   "metal",
			Count:  metalCount,
		},
		{
			ID:     "cpu",
			Label:  "CPU",
			Detail: "fallback",
			Kind:   "cpu",
			Count:  cpuCount,
		},
	}

	links := []SystemTopologyLink{
		{From: "machine", To: "stream"},
		{From: "stream", To: "emitter"},
		{From: "emitter", To: "backend"},
		{From: "backend", To: "pool"},
		{From: "pool", To: "machine"},
		{From: "backend", To: "cuda"},
		{From: "backend", To: "metal"},
		{From: "backend", To: "cpu"},
	}

	return SystemTopology{
		Title:         "SYSTEM",
		Subtitle:      "machine · stream · emitter · backend · pool",
		StreamRegions: defaultStreamRegions,
		Core:          core,
		Hardware:      hardware,
		Links:         links,
	}
}

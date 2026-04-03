package visualizer

import (
	"fmt"
	"runtime"

	"github.com/theapemachine/six/pkg/compute/kernel/cuda"
	"github.com/theapemachine/six/pkg/compute/kernel/metal"
)

/*
SystemTopology snapshots how the browser should think about the runtime.
Machine and tokenizer implement the io loop; cluster.ControlPlane holds
Kademlia routing and per-bucket LSM (store.SpatialIndex); compute.Backend owns
ingress FIFOs, batching, executeBatch, optional pool, and substrate routing.

IDs emitter and pool stay in the JSON for stable viz zone keys even though
they are logical sub-blocks of the backend in the 3D layout.
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
	backendSub := "substrates"
	if backendCount == 1 {
		backendSub = "substrate"
	}
	poolNoun := "workers"
	if poolWorkers == 1 {
		poolNoun = "worker"
	}

	core := []SystemTopologyNode{
		{
			ID:     "machine",
			Label:  "Machine",
			Detail: "Read → Backend.Queue; Write → tokenizer",
			Kind:   "machine",
			Count:  1,
		},
		{
			ID:     "stream",
			Label:  "Tokenizer",
			Detail: "ring · io.Pipe into Machine.Read",
			Kind:   "stream",
			Count:  1,
		},
		{
			ID:     "controlplane",
			Label:  "Control plane",
			Detail: "Kademlia routing table · bucket LSM (pkg/cluster, pkg/store)",
			Kind:   "controlplane",
			Count:  1,
		},
		{
			ID:    "backend",
			Label: "Backend",
			Detail: fmt.Sprintf(
				"NORMAL/PRIORITY queues · %d pool %s · gather · executeBatch · %d %s",
				poolWorkers,
				poolNoun,
				backendCount,
				backendSub,
			),
			Kind:  "backend",
			Count: backendCount,
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
			Detail: "interpreter / SIMD UniversalBitwise",
			Kind:   "cpu",
			Count:  cpuCount,
		},
	}

	links := []SystemTopologyLink{
		{From: "machine", To: "stream"},
		{From: "stream", To: "machine"},
		{From: "stream", To: "controlplane"},
		{From: "controlplane", To: "backend"},
		{From: "machine", To: "emitter"},
		{From: "emitter", To: "backend"},
		{From: "backend", To: "pool"},
		{From: "pool", To: "machine"},
		{From: "backend", To: "cuda"},
		{From: "backend", To: "metal"},
		{From: "backend", To: "cpu"},
	}

	return SystemTopology{
		Title:         "SYSTEM",
		Subtitle:      "machine · tokenizer · Kademlia/LSM · backend (queue·pool)",
		StreamRegions: defaultStreamRegions,
		Core:          core,
		Hardware:      hardware,
		Links:         links,
	}
}

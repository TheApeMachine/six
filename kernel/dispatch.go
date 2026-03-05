package kernel

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/kernel/cpu"
	"github.com/theapemachine/six/kernel/cuda"
	"github.com/theapemachine/six/kernel/metal"
	"github.com/theapemachine/six/numeric"
)

type backendRunner struct {
	name      string
	run       func(unsafe.Pointer, int, unsafe.Pointer, unsafe.Pointer, int, unsafe.Pointer) (uint64, error)
	available func() bool
}

var defaultBackendOrder = []string{"cuda", "metal", "cpu"}

func BestFill(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	targetIdx int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error) {
	if numChords == 0 {
		return 0, 0.0, nil
	}

	if expectedReality == nil {
		expectedReality = context
	}

	workers := distributedWorkersFromEnv()
	if len(workers) > 0 {
		if packed, err := bestFillDistributedPacked(workers, dictionary, numChords, context, expectedReality, targetIdx, geodesicLUT); err == nil {
			idx, score := numeric.DecodePacked(packed)
			return idx, score, nil
		}
	}

	packed, err := BestFillLocalPacked(dictionary, numChords, context, expectedReality, targetIdx, geodesicLUT)
	if err != nil {
		return 0, 0.0, err
	}

	idx, score := numeric.DecodePacked(packed)
	return idx, score, nil
}

func BestFillLocalPacked(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	targetIdx int,
	geodesicLUT unsafe.Pointer,
) (uint64, error) {
	if dictionary == nil {
		return 0, fmt.Errorf("dictionary pointer is nil")
	}
	if context == nil {
		return 0, fmt.Errorf("context pointer is nil")
	}
	if expectedReality == nil {
		expectedReality = context
	}

	backends := configuredBackends()
	if len(backends) == 0 {
		return cpu.BestFillCPUPacked(dictionary, numChords, context, expectedReality, targetIdx, geodesicLUT)
	}

	if !envBool("SIX_HETERO_LOCAL", false) || len(backends) == 1 || numChords < envInt("SIX_LOCAL_SHARD_THRESHOLD", 4096) {
		return runBackendWithFallback(backends[0], dictionary, numChords, context, expectedReality, targetIdx, geodesicLUT)
	}

	chunkSize := envInt("SIX_LOCAL_CHUNK", 2048)
	if chunkSize < 256 {
		chunkSize = 256
	}

	var next atomic.Int64
	var best atomic.Uint64
	errCh := make(chan error, len(backends))
	var wg sync.WaitGroup

	for _, backend := range backends {
		backend := backend
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				start := int(next.Add(int64(chunkSize)) - int64(chunkSize))
				if start >= numChords {
					return
				}

				end := min(start+chunkSize, numChords)
				shardPtr := unsafe.Pointer(uintptr(dictionary) + uintptr(start*numeric.ManifoldBytes))
				packed, err := runBackendWithFallback(
					backend,
					shardPtr,
					end-start,
					context,
					expectedReality,
					targetIdx-start,
					geodesicLUT,
				)
				if err != nil {
					errCh <- err
					return
				}

				packed = numeric.RebasePackedID(packed, start)
				atomicMaxPacked(&best, packed)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	if err, ok := <-errCh; ok {
		return 0, err
	}

	return best.Load(), nil
}

func runBackendWithFallback(
	backend backendRunner,
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	targetIdx int,
	geodesicLUT unsafe.Pointer,
) (uint64, error) {
	packed, err := backend.run(dictionary, numChords, context, expectedReality, targetIdx, geodesicLUT)
	if err == nil {
		return packed, nil
	}
	if backend.name == "cpu" {
		return 0, err
	}
	return cpu.BestFillCPUPacked(dictionary, numChords, context, expectedReality, targetIdx, geodesicLUT)
}

func configuredBackends() []backendRunner {
	runners := map[string]backendRunner{
		"cuda": {
			name:      "cuda",
			run:       cuda.BestFillCUDAPacked,
			available: cuda.CudaAvailable,
		},
		"metal": {
			name:      "metal",
			run:       metal.BestFillMetalPacked,
			available: metal.MetalAvailable,
		},
		"cpu": {
			name: "cpu",
			run: func(dictionary unsafe.Pointer, numChords int, context unsafe.Pointer, expectedReality unsafe.Pointer, targetIdx int, geodesicLUT unsafe.Pointer) (uint64, error) {
				return cpu.BestFillCPUPacked(dictionary, numChords, context, expectedReality, targetIdx, geodesicLUT)
			},
			available: func() bool { return true },
		},
	}

	order := backendOrderFromEnv()
	result := make([]backendRunner, 0, len(order))
	for _, name := range order {
		runner, ok := runners[name]
		if !ok {
			continue
		}
		if runner.available != nil && runner.available() {
			result = append(result, runner)
		}
	}

	return result
}

func backendOrderFromEnv() []string {
	configured := strings.TrimSpace(os.Getenv("SIX_BACKENDS"))
	if configured == "" {
		return defaultBackendOrder
	}

	parts := strings.Split(configured, ",")
	result := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}

	if len(result) == 0 {
		return defaultBackendOrder
	}

	return result
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func atomicMaxPacked(dst *atomic.Uint64, val uint64) {
	for {
		current := dst.Load()
		if val <= current {
			return
		}
		if dst.CompareAndSwap(current, val) {
			return
		}
	}
}

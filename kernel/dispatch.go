package kernel

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel/cpu"
	"github.com/theapemachine/six/kernel/cuda"
	"github.com/theapemachine/six/kernel/metal"
	"github.com/theapemachine/six/numeric"
)

type backendRunner struct {
	name string
	run  func(
		dictionary unsafe.Pointer,
		numChords int,
		context unsafe.Pointer,
		expectedReality unsafe.Pointer,
		expectedPrecision unsafe.Pointer,
		geodesicLUT unsafe.Pointer,
	) (uint64, error)
	available func() bool
}

var defaultBackendOrder = []string{"cuda", "metal", "cpu"}

type SpanMatch struct {
	Index int
	Score float64
}

func BestSpan(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) (SpanMatch, error) {
	return BestSpanWithPrecision(dictionary, numChords, context, expectedReality, nil, geodesicLUT)
}

func BestSpanWithPrecision(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) (SpanMatch, error) {
	if numChords == 0 {
		return SpanMatch{Index: -1}, nil
	}

	if expectedReality == nil {
		expectedReality = context
	}

	workers := distributedWorkersFromEnv()

	if len(workers) > 0 {
		if packed, err := bestFillDistributedPacked(
			workers,
			dictionary,
			numChords,
			context,
			expectedReality,
			expectedPrecision,
			geodesicLUT,
		); err == nil {
			idx, score := numeric.DecodePacked(packed)
			return SpanMatch{
				Index: idx,
				Score: score,
			}, nil
		}
	}

	packed, err := BestFillLocalPacked(
		dictionary,
		numChords,
		context,
		expectedReality,
		expectedPrecision,
		geodesicLUT,
	)

	if err != nil {
		return SpanMatch{}, err
	}

	idx, score := numeric.DecodePacked(packed)
	return SpanMatch{
		Index: idx,
		Score: score,
	}, nil
}

func BestFill(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	_ int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error) {
	return BestFillWithPrecision(dictionary, numChords, context, expectedReality, nil, geodesicLUT)
}

func HolographicRecall(
	substrateFilters []data.Chord,
	primeField unsafe.Pointer,
	targetRot geometry.GFRotation,
) (SpanMatch, error) {
	if len(substrateFilters) == 0 {
		return SpanMatch{Index: -1}, nil
	}

	targetRotA := uint32(targetRot.A)
	targetRotB := uint32(targetRot.B)

	var filtersPtr unsafe.Pointer
	if len(substrateFilters) > 0 {
		filtersPtr = unsafe.Pointer(&substrateFilters[0])
	}

	packed, err := metal.HolographicRecallMetalPacked(
		filtersPtr,
		len(substrateFilters),
		primeField,
		targetRotA,
		targetRotB,
	)

	if err != nil {
		return SpanMatch{}, err
	}

	idx, score := numeric.DecodePacked(packed)
	return SpanMatch{
		Index: idx,
		Score: score,
	}, nil
}

func BestFillWithExpectedField(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedField *geometry.ExpectedField,
	geodesicLUT unsafe.Pointer,
) (int, float64, error) {
	var precisionPtr unsafe.Pointer
	if expectedField != nil {
		precisionPtr = unsafe.Pointer(&expectedField.Precision[0][0])
	}

	return BestFillWithPrecision(dictionary, numChords, context, expectedReality, precisionPtr, geodesicLUT)
}

func BestFillWithPrecision(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) (int, float64, error) {
	match, err := BestSpanWithPrecision(
		dictionary,
		numChords,
		context,
		expectedReality,
		expectedPrecision,
		geodesicLUT,
	)
	if err != nil {
		return 0, 0.0, err
	}

	if match.Index < 0 {
		return 0, 0.0, nil
	}

	return match.Index, match.Score, nil
}

func BestFillLocalPacked(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	expectedPrecision unsafe.Pointer,
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
		return cpu.BestFillCPUPacked(
			dictionary,
			numChords,
			context,
			expectedReality,
			expectedPrecision,
			geodesicLUT,
		)
	}

	if !config.System.HeteroLocal ||
		len(backends) == 1 ||
		numChords < config.System.LocalShardThreshold {
		return runBackendWithFallback(
			backends[0],
			dictionary,
			numChords,
			context,
			expectedReality,
			expectedPrecision,
			geodesicLUT,
		)
	}

	chunkSize := max(config.System.Chunk, config.Numeric.NSymbols)

	var next atomic.Int64
	var best atomic.Uint64
	errCh := make(chan error, len(backends))
	var wg sync.WaitGroup

	for _, backend := range backends {
		wg.Go(func() {
			for {
				start := int(next.Add(int64(chunkSize)) - int64(chunkSize))

				if start >= numChords {
					return
				}

				end := min(start+chunkSize, numChords)
				shardPtr := unsafe.Add(
					dictionary, (start * numeric.ManifoldBytes),
				)

				packed, err := runBackendWithFallback(
					backend,
					shardPtr,
					end-start,
					context,
					expectedReality,
					expectedPrecision,
					geodesicLUT,
				)
				if err != nil {
					errCh <- err
					return
				}

				packed = numeric.RebasePackedID(packed, start)
				atomicMaxPacked(&best, packed)
			}
		})
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
	expectedPrecision unsafe.Pointer,
	geodesicLUT unsafe.Pointer,
) (uint64, error) {
	packed, err := backend.run(
		dictionary,
		numChords,
		context,
		expectedReality,
		expectedPrecision,
		geodesicLUT,
	)

	if err == nil {
		return packed, nil
	}

	if backend.name == "cpu" {
		return 0, err
	}

	return cpu.BestFillCPUPacked(
		dictionary,
		numChords,
		context,
		expectedReality,
		expectedPrecision,
		geodesicLUT,
	)
}

func configuredBackends() []backendRunner {
	runners := map[string]backendRunner{
		"cuda": {
			name: "cuda",
			run: func(
				dictionary unsafe.Pointer,
				numChords int,
				context unsafe.Pointer,
				expectedReality unsafe.Pointer,
				expectedPrecision unsafe.Pointer,
				geodesicLUT unsafe.Pointer,
			) (uint64, error) {
				return cuda.BestFillCUDAPacked(
					dictionary,
					numChords,
					context,
					expectedReality,
					expectedPrecision,
					geodesicLUT,
				)
			},
			available: cuda.CudaAvailable,
		},
		"metal": {
			name: "metal",
			run: func(
				dictionary unsafe.Pointer,
				numChords int,
				context unsafe.Pointer,
				expectedReality unsafe.Pointer,
				expectedPrecision unsafe.Pointer,
				geodesicLUT unsafe.Pointer,
			) (uint64, error) {
				return metal.BestFillMetalPacked(
					dictionary,
					numChords,
					context,
					expectedReality,
					expectedPrecision,
					geodesicLUT,
				)
			},
			available: metal.MetalAvailable,
		},
		"cpu": {
			name: "cpu",
			run: func(
				dictionary unsafe.Pointer,
				numChords int,
				context unsafe.Pointer,
				expectedReality unsafe.Pointer,
				expectedPrecision unsafe.Pointer,
				geodesicLUT unsafe.Pointer,
			) (uint64, error) {
				return cpu.BestFillCPUPacked(
					dictionary,
					numChords,
					context,
					expectedReality,
					expectedPrecision,
					geodesicLUT,
				)
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

package data

import (
	"context"
	"fmt"
	"math/bits"

	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
FlatValue is a dense array of active prime indices used for optimal GPU iteration.
It eliminates bit-twiddling thread divergence in SIMT architectures.
*/
type FlatValue struct {
	ActivePrimes [config.NBasis]uint16
	Count        uint16
	_            uint16 // Padding
}

/*
Flatten converts the sparse bitset into a densely packed array of active prime indices.
*/
func (value *Value) Flatten() FlatValue {
	var flat FlatValue
	primes := &flat.ActivePrimes
	count := uint16(0)

	for i := range config.ValueBlocks {
		block := value.block(i)

		for block != 0 {
			bit := uint16(bits.TrailingZeros64(block))
			primes[count] = uint16(i<<6) + bit
			count++
			block &= block - 1
		}
	}

	flat.Count = count
	return flat
}

/*
FlattenBatched converts a slice of sparse Values into a slice of FlatValues.
If a pool is provided, each value is scheduled as an independent task and the
pool's built-in scaler handles concurrency — no manual worker-count tuning.
Falls back to synchronous execution when no pool is available.
*/
func FlattenBatched(values []Value, p *pool.Pool) []FlatValue {
	n := len(values)
	out := make([]FlatValue, n)

	if n == 0 {
		return out
	}

	if p == nil {
		for i := range values {
			out[i] = values[i].Flatten()
		}
		return out
	}

	resChs := make([]chan *pool.Result, 0, n)

	for i := range values {
		idx := i
		resCh := p.Schedule(fmt.Sprintf("flatten-%d", idx), func(ctx context.Context) (any, error) {
			out[idx] = values[idx].Flatten()
			return nil, nil
		})

		if resCh == nil {
			out[idx] = values[idx].Flatten()
			continue
		}

		resChs = append(resChs, resCh)
	}

	for _, ch := range resChs {
		<-ch
	}

	return out
}

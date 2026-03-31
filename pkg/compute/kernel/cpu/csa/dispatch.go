// Copyright (c) 2020, 2024 Robert Clausecker <fuz@fuz.su>
// Trimmed for six: uint64 positional population count only.

/*
Package pospop implements positional population count over []uint64: for each bit
lane j in 0..63, counts[j] receives how many input words had that bit set.

Assembly backends match pkg/compute/kernel/cpu: amd64 (AVX2 when available, else SSE2),
arm64 (NEON), and generic scalar elsewhere.
*/
package pospop

type count64impl struct {
	count64   func(*[64]int, []uint64)
	name      string
	available bool
}

var count64func = func() func(*[64]int, []uint64) {
	for _, candidate := range count64funcs {
		if candidate.available {
			return candidate.count64
		}
	}

	panic("pospop: no count64 implementation available")
}()

/*
Count64 adds, for each index j in 0..63, the number of elements in buf with bit j set.
*/
func Count64(counts *[64]int, buf []uint64) {
	count64func(counts, buf)
}

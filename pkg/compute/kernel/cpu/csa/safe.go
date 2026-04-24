// Copyright (c) 2020 Robert Clausecker <fuz@fuz.su>

package pospop

// count64safe is the naive reference implementation for tests.
func count64safe(counts *[64]int, buf []uint64) {
	for wordIndex := range buf {
		for bit := 0; bit < 64; bit++ {
			counts[bit] += int(buf[wordIndex] >> bit & 1)
		}
	}
}

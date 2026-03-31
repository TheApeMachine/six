// Copyright (c) 2020--2022, 2025 Robert Clausecker <fuz@fuz.su>

package pospop

import (
	"math/rand"
	"strconv"
	"testing"
)

var benchmarkLengths = []int{
	1, 10, 100, 1000, 10 * 1000, 100 * 1000, 1000 * 1000, 10 * 1000 * 1000, 100 * 1000 * 1000,
}

var benchmarkLengthsShort = []int{100 * 1000}

func benchmarkCount64(b *testing.B, buf []uint64, lengths []int, count64 func(*[64]int, []uint64)) {
	for _, length := range lengths {
		b.Run(strconv.Itoa(length), func(bb *testing.B) {
			var counts [64]int
			testbuf := buf[:length/8]
			bb.SetBytes(int64(length))
			for iteration := 0; iteration < bb.N; iteration++ {
				count64(&counts, testbuf)
			}
		})
	}
}

func BenchmarkCount64(b *testing.B) {
	funcs := count64funcs
	lengths := benchmarkLengths

	if testing.Short() {
		funcs = []count64impl{{Count64, "dispatch", true}}
		lengths = benchmarkLengthsShort
	}

	maxlen := lengths[len(lengths)-1] / 8
	buf := make([]uint64, maxlen+1)
	for index := range buf {
		buf[index] = rand.Uint64()
	}
	buf = buf[1:]

	for _, impl := range funcs {
		b.Run(impl.name, func(bb *testing.B) {
			if !impl.available {
				bb.SkipNow()
			}

			benchmarkCount64(bb, buf, lengths, impl.count64)
		})
	}
}

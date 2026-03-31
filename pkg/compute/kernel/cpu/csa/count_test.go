// Copyright (c) 2020--2022, 2024 Robert Clausecker <fuz@fuz.su>

package pospop

import (
	"math/rand"
	"testing"
)

var testLengths = []int{
	0, 1, 2, 3,
	4, 5, 6, 7,
	8, 9, 10, 11,
	12, 13, 14, 15,
	16, 17, 18, 19,
	31, 32, 33,
	63, 64, 65,
	95, 97, 98,
	119, 120, 121,
	239, 240, 241,
	2*240 - 1, 2 * 240, 2*240 + 1,
	4*240 - 1, 4 * 240, 4*240 + 1,
	1023, 1024, 1025,
	(15 + 16) * 8, (15 + 16) * 16, (15 + 16) * 32, (15 + 16) * 64,
	(255*16 + 15) * 64,
}

const minimizationThreshold = (15 + 16) * 64

func randomCounts(counts []int) {
	for index := range counts {
		counts[index] = rand.Int()
	}
}

func countDiff(left, right []int) []int {
	res := make([]int, len(left))
	for index := range left {
		res[index] = right[index] - left[index]
	}
	return res
}

func testCount64(t *testing.T, count64 func(*[64]int, []uint64)) {
	for _, length := range testLengths {
		buf := make([]uint64, length+1)
		buf = buf[1 : length+1]
		for index := range buf {
			buf[index] = rand.Uint64()
		}

		var counts [64]int
		randomCounts(counts[:])
		refCounts := counts

		count64(&counts, buf)
		count64safe(&refCounts, buf)

		if counts != refCounts {
			t.Errorf("length %d: counts don't match: %v\n", length, countDiff(counts[:], refCounts[:]))

			if length > minimizationThreshold {
				continue
			}

			min := minimizeTestcase64(count64, buf)
			tcstr := testcaseString64(min)
			if tcstr != "" {
				t.Log("minimized test case:\n", tcstr)
			}
		}
	}
}

func TestCount64(t *testing.T) {
	t.Run("dispatch", func(tt *testing.T) { testCount64(tt, Count64) })

	for index := range count64funcs {
		t.Run(count64funcs[index].name, func(tt *testing.T) {
			if !count64funcs[index].available {
				tt.SkipNow()
			}

			testCount64(tt, count64funcs[index].count64)
		})
	}
}

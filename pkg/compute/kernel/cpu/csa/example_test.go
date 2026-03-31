// Copyright (c) 2020 Robert Clausecker <fuz@fuz.su>

package pospop_test

import (
	"fmt"

	pospop "github.com/theapemachine/six/pkg/compute/kernel/cpu/csa"
)

func ExampleCount64() {
	var counts [64]int
	words := []uint64{
		1,
		2,
		3,
		5,
		6,
		9,
	}
	pospop.Count64(&counts, words)
	fmt.Println(counts[0], counts[1], counts[2], counts[3])
	// Output: 4 3 2 1
}

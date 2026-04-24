// Copyright (c) 2020, 2024 Robert Clausecker <fuz@fuz.su>

package pospop

func count64neon(counts *[64]int, buf []uint64)

var count64funcs = []count64impl{
	{count64neon, "neon", true},
	{count64generic, "generic", true},
}

// Copyright (c) 2020 Robert Clausecker <fuz@fuz.su>

package pospop

import "golang.org/x/sys/cpu"

func count64avx2(counts *[64]int, buf []uint64)
func count64sse2(counts *[64]int, buf []uint64)

// Dispatch order matches cpu.execWordBlock: prefer AVX2 when present, else SSE2, else generic.
var count64funcs = []count64impl{
	{count64avx2, "avx2", cpu.X86.HasAVX2},
	{count64sse2, "sse2", cpu.X86.HasSSE2},
	{count64generic, "generic", true},
}

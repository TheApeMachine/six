package kernel

/*
ApplyCopyMaskMerge performs per-word masked merge from program operand refs:
dst[i] = (srcA[i] & srcB[i]) | (dst[i] & ^srcB[i]) for i in 0..min(spans)-1.
The program opcode byte must be OpcodeCopyMaskMerge; srcA/srcB/dst come from
the usual packed region words at ProgramSrcAWord..ProgramDstWord.
*/
func ApplyCopyMaskMerge(frame *[128]uint64) {
	if frame == nil {
		return
	}

	aStart, aSpan := UnpackRegionRef(frame[ProgramSrcAWord])
	bStart, bSpan := UnpackRegionRef(frame[ProgramSrcBWord])
	dstStart, dstSpan := UnpackRegionRef(frame[ProgramDstWord])

	n := aSpan
	if bSpan < n {
		n = bSpan
	}

	if dstSpan < n {
		n = dstSpan
	}

	if n <= 0 {
		return
	}

	if aStart < 0 || bStart < 0 || dstStart < 0 {
		return
	}

	if aStart+n > len(frame) || bStart+n > len(frame) || dstStart+n > len(frame) {
		return
	}

	for idx := 0; idx < n; idx++ {
		mask := frame[bStart+idx]
		src := frame[aStart+idx]
		dst := dstStart + idx

		frame[dst] = (src & mask) | (frame[dst] & ^mask)
	}
}

/*
ApplyPostExecutionLifecycle decrements PropertiesTTLWord after an ALU pass and
clears scheduling when TTL exhausts. When TTL transitions from 1 to exhausted,
it writes TTLExpiredSentinel so host finalize can skip orchestrator publish.
*/
func ApplyPostExecutionLifecycle(frame *[128]uint64) {
	if frame == nil {
		return
	}

	word := frame[PropertiesTTLWord]
	if word == 0 || word == ^uint64(0) {
		return
	}

	if word&TTLExpiredSentinel != 0 {
		return
	}

	if word == 1 {
		frame[PropertiesTTLWord] = TTLExpiredSentinel
		frame[SchedulingNextProgramWord] = 0

		return
	}

	frame[PropertiesTTLWord] = word - 1
}

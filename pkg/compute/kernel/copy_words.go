package kernel

/*
CopyWordsInFrame copies n contiguous words inside one Value frame. Overlap is
handled like memmove. Used by finalizer-style region copies without touching
the program opcode words.
*/
func CopyWordsInFrame(frame *[128]uint64, srcStart, dstStart, n int) {
	if frame == nil || n <= 0 {
		return
	}

	if srcStart < 0 || dstStart < 0 {
		return
	}

	if srcStart+n > len(frame) || dstStart+n > len(frame) {
		return
	}

	if srcStart == dstStart {
		return
	}

	if srcStart < dstStart && srcStart+n > dstStart {
		for idx := n - 1; idx >= 0; idx-- {
			frame[dstStart+idx] = frame[srcStart+idx]
		}

		return
	}

	for idx := 0; idx < n; idx++ {
		frame[dstStart+idx] = frame[srcStart+idx]
	}
}

/*
CopyWordsBetween copies n words from srcFrame into dstFrame. No overlap case
across distinct frames.
*/
func CopyWordsBetween(dstFrame, srcFrame *[128]uint64, dstStart, srcStart, n int) {
	if dstFrame == nil || srcFrame == nil || n <= 0 {
		return
	}

	if srcStart < 0 || dstStart < 0 {
		return
	}

	if srcStart+n > len(srcFrame) || dstStart+n > len(dstFrame) {
		return
	}

	if dstFrame == srcFrame {
		CopyWordsInFrame(dstFrame, srcStart, dstStart, n)

		return
	}

	for idx := 0; idx < n; idx++ {
		dstFrame[dstStart+idx] = srcFrame[srcStart+idx]
	}
}

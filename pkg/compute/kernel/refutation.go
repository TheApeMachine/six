package kernel

/*
ApplyRefutationProbe runs after the ALU has written signals. When
PropertiesRefutationTargetWord names a non-zero hypothesis ID and the
longest one-run in the signal region exceeds RefutationOneRunThreshold, the
probe sets FalsifiedBitNoiseWord in PropertiesNoiseWord and clears scheduling.
*/
func ApplyRefutationProbe(frame *[128]uint64) {
	if frame == nil {
		return
	}

	target := frame[PropertiesRefutationTargetWord]
	if target == 0 {
		return
	}

	words := frame[SignalsStartWord : SignalsStartWord+8]
	if longestOneRunInWords(words) < RefutationOneRunThreshold {
		return
	}

	frame[PropertiesNoiseWord] |= FalsifiedBitNoiseWord
	frame[SchedulingNextProgramWord] = 0
	frame[PropertiesRefutationTargetWord] = 0
}

/*
longestOneRunInWords mirrors geometry.ScanOneRun’s length result for a fixed
slice without importing geometry (avoids package cycles).
*/
func longestOneRunInWords(words []uint64) int {
	bestLen := 0
	curLen := 0

	for _, word := range words {
		if word == ^uint64(0) {
			curLen += 64

			continue
		}

		if word == 0 {
			if curLen > bestLen {
				bestLen = curLen
			}

			curLen = 0

			continue
		}

		for bit := 0; bit < 64; bit++ {
			if (word>>bit)&1 == 1 {
				curLen++
			} else {
				if curLen > bestLen {
					bestLen = curLen
				}

				curLen = 0
			}
		}
	}

	if curLen > bestLen {
		bestLen = curLen
	}

	return bestLen
}

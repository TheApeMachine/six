package stepwise

import (
	"github.com/theapemachine/six/pkg/core"
)

/*
FrameWords is the fixed word count of one Value frame; must match primitive.Words.
*/
const FrameWords = 128

/*
DefaultProgramWordBase matches the embedded program band in cmd/cfg/config.yml
when viper has not overridden value.region.program.start.
*/
const DefaultProgramWordBase = 76

/*
ProgramWordsAvailable returns how many uint64 step descriptors fit in the tail
of a frame when the embedded program starts at DefaultProgramWordBase.
*/
func ProgramWordsAvailable() int {
	return FrameWords - DefaultProgramWordBase
}

/*
EmbeddedProgramBase returns the configured program word index, falling back to
DefaultProgramWordBase when unset or out of range.
*/
func EmbeddedProgramBase() int {
	start := core.Cfg.Value.Region.Program.Start

	if start <= 0 || start >= FrameWords {
		return DefaultProgramWordBase
	}

	return start
}

/*
EmbeddedStepCount returns the number of consecutive words in the program band
starting at EmbeddedProgramBase through the end of the frame (header + payload).
*/
func EmbeddedStepCount() int {
	return FrameWords - EmbeddedProgramBase()
}

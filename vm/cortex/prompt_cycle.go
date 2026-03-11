package cortex

import "github.com/theapemachine/six/data"

type PromptCycle struct {
	ID     uint64
	Chords []data.Chord
}

type PromptLogic struct {
	PromptID uint64
	Snapshot LogicSnapshot
}

type PromptResult struct {
	PromptID uint64
	Chords   []data.Chord
}

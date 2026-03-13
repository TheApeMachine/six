package vm

import (
	"context"

	"github.com/theapemachine/six/pkg/data"
)

/*
Cortex is any System that can accept chords and return paths through the
real Prompt→SpatialLookup→Evaluate→RecursiveFold pipeline.
*/
type Cortex interface {
	System
	PromptChords(ctx context.Context, chords data.Chord_List) ([][]data.Chord, error)
}

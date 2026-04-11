package programmer

import "github.com/theapemachine/six/pkg/primitive"

/*
ContinuationKind classifies a trailing next-program directive after the last
operation line.
*/
type ContinuationKind uint8

const (
	ContinuationNone ContinuationKind = iota
	ContinuationValueID
	ContinuationSelf
)

/*
schedulingNextProgramWord is the last word of the reserved band (56–117) where
Executable records the next program ValueID after Values exist.
*/
const schedulingNextProgramWord = 117

/*
Continuation records scheduling intent applied by Executable after Values exist
(word 117 holds the next program ValueID when Kind is ContinuationValueID).
*/
type Continuation struct {
	Kind    ContinuationKind
	ValueID uint64
}

/*
ApplyScheduling writes word-117 scheduling metadata onto a materialized Value.
*/
func (continuation *Continuation) ApplyScheduling(value *primitive.Value) {
	if value == nil || continuation == nil || continuation.Kind == ContinuationNone {
		return
	}

	switch continuation.Kind {
	case ContinuationValueID:
		value.Set(schedulingNextProgramWord, continuation.ValueID)
	case ContinuationSelf:
		value.Set(schedulingNextProgramWord, value.ID())
	default:
	}
}

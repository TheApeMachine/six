package lang

import (
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
)

/*
Output captures the results of a single Program run against a dataset.
*/
type Output struct {
	QueryMask      primitive.Value
	Matches        []primitive.MatchResult
	WinnerIndex    int
	RecoveredState primitive.Value
	PostResidue    int
	Steps          int
	Candidate      *macro.ProgramCandidate
}

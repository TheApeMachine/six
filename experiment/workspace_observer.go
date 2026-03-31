package experiment

/*
WorkspaceTokenObserver marks experiments whose graded Observed bytes are defined
as the post-learn token workspace (see primitive.TokenRegionObservedBytes), not
Value.String() / exact LSM frame equality on the prompt Value.

The pipeline always runs learn on a pair of copies before packing tokens for
HoldoutScorer; this interface is documentation + hook for future inverse-HV
decode variants.
*/
type WorkspaceTokenObserver interface {
	ObserveWorkspaceAsTokens() bool
}

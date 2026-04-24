package experiment

/*
WorkspaceTokenObserver marks experiments whose graded Observed bytes are defined
as the post-learn token workspace (see primitive.TokenRegionObservedBytes), not
Value.String() / exact LSM frame equality on the prompt Value.

The pipeline runs learn on a Value pair, then packs the self token region for
HoldoutScorer. When a holdout exists, the partner encodes the supervised
concatenation prompt+holdout so XOR does not degenerate to a full cancel on
identical frames. With no holdout the partner is still a copy (degenerate
cancel). This interface is documentation + hook for future inverse-HV decode
variants.
*/
type WorkspaceTokenObserver interface {
	ObserveWorkspaceAsTokens() bool
}

package experiment

/*
HoldoutProvider is implemented by experiments that pair each pipeline prompt
index with optional holdout bytes used by HoldoutScorer (Enrich compares
Holdout vs Observed). Observed is normally the raw Value frame unless the
experiment also implements WorkspaceTokenObserver.

The pipeline supplies holdouts when building ExperimentalData so AddResult
receives non-empty Holdout without each experiment re-deriving indices.
*/
type HoldoutProvider interface {
	HoldoutForPrompt(idx int) ([]byte, bool)
}

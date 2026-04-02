package experiment

/*
HoldoutSuffixFromSample returns the trailing holdout slice for a prompt sample.

holdoutBytes is sampleLen*holdoutPct/100 (same formula as RuleShift and
ConstraintResolution). An empty sample yields false. When the holdout would
cover the entire prompt, the full sample is copied.
*/
func HoldoutSuffixFromSample(sample []byte, holdoutPct, sampleLen int) ([]byte, bool) {
	if len(sample) == 0 {
		return nil, false
	}

	holdoutBytes := sampleLen * holdoutPct / 100

	if holdoutBytes >= len(sample) {
		return append([]byte(nil), sample...), true
	}

	return append([]byte(nil), sample[len(sample)-holdoutBytes:]...), true
}

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

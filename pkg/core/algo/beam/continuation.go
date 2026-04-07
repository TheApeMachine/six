package beam

/*
BeamContinuation is one completed beam path: the generated surface string and
the accumulated log-probability score (natural log) along the tokens that were
actually emitted, excluding the configured end marker from the visible text.
*/
type Continuation struct {
	Sequence string
	Score    float64
}

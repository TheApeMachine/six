package experiment

/*
WorkspaceTokenObserver marks experiments where the graded answer should be read
from the workspace token region (DecodeTokensToText) after each prompt Write,
instead of the raw 1024-byte wire frame.

In-band programs (e.g. query) should write the final answer into token slots;
HoldoutScorer then compares Holdout to that decoded text.
*/
type WorkspaceTokenObserver interface {
	ObserveWorkspaceAsTokens() bool
}

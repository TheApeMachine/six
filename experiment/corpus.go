package experiment

/*
CorpusProvider stages resident corpus samples in the spatial index before the
pipeline starts grading prompts. Samples must be corpus-side evidence only;
they must not be synthesized from prompt-time holdout leakage.
*/
type CorpusProvider interface {
	CorpusSamples() [][]byte
}

/*
CorpusObserver derives Observed bytes from prompt bytes plus the already staged
corpus. excludeValueID is the ephemeral prompt Value inserted for the current
prompt; observers must ignore it so the prompt cannot retrieve itself.
*/
type CorpusObserver interface {
	ObserveFromCorpus(prompt []byte, excludeValueID uint64) ([]byte, error)
}

/*
CorpusRegistrar receives each resident corpus sample together with the ValueID
assigned during staging so experiments can map retrieval hits back to corpus
metadata without consulting prompt-time holdouts.
*/
type CorpusRegistrar interface {
	RegisterCorpusSample(valueID uint64, sample []byte)
}

package frankentrie

/*
Surprisal holds one token surprisal in bits.
*/
type Surprisal struct {
	Token string
	Bits  float64
}

/*
BeamCandidate is one beam-search completion.
*/
type BeamCandidate struct {
	Sequence string
	Score    float64
}

/*
ExtractedSymbol describes a label-skewed repeated sequence fragment.
*/
type ExtractedSymbol struct {
	Symbol string
	Label  string
	Score  float64
}

type episodicEvent struct {
	ID     string
	Tokens []string
	Label  string
	Step   int
}

/*
EpisodicEpisode is a snapshot of one row in the rolling episodic buffer (RAG tail).
*/
type EpisodicEpisode struct {
	ID        string
	Tokens    []string
	Label     string
	Timestamp int
}

/*
TokenContribution is one step in the per-label log-prob trace used for contrastive
explanations (demo-style contributions / winner vs runner-up plots).
*/
type TokenContribution struct {
	Token   string
	LogProb float64
}

/*
BytePairEncoder is a byte-pair merge tokenizer trained from raw lines. It emits
subword strings derived from rune splits plus an end-of-word marker so that
word boundaries stay recoverable when subwords merge aggressively.
*/
type BytePairEncoder struct {
	mergeRank map[string]int
}

type beamState struct {
	Tokens []string
	Score  float64
}

func isSeparatorRune(value rune) bool {
	return value == '_' || value == ' '
}

func isSeparatorToken(token string) bool {
	if token == "" {
		return false
	}

	for _, value := range token {
		if !isSeparatorRune(value) {
			return false
		}
	}

	return true
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

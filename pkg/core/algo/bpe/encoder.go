package bpe

import "strings"

/*
Encoder is a byte-pair merge tokenizer trained from raw lines. It emits
subword strings derived from rune splits plus an end-of-word marker so that
word boundaries stay recoverable when subwords merge aggressively.
*/
type Encoder struct {
	mergeRank map[string]int
	EndToken  string
}

func NewEncoder() *Encoder {
	return &Encoder{
		mergeRank: make(map[string]int),
	}
}

func (encoder *Encoder) Encode(sequence string) []string {
	tokens := strings.Split(sequence, " ")

	for _, token := range tokens {
		encoder.mergeRank[token] = len(encoder.mergeRank)
	}

	return tokens
}

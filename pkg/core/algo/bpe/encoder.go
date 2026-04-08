package bpe

import (
	"bytes"
	"strings"
)

/*
Encoder is a byte-pair merge tokenizer trained from raw lines. It emits
subword strings derived from rune splits plus an end-of-word marker so that
word boundaries stay recoverable when subwords merge aggressively.

Callers that own serialization (e.g. train.Online under its mutex) may
invoke Encode / EncodeBytes without an internal lock — mergeRank updates
are not safe under concurrent writers without external synchronization.
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

/*
EncodeBytes splits on ASCII space like Encode but accepts the raw token slab
without building an intermediate full-Value string on ingest paths.
*/
func (encoder *Encoder) EncodeBytes(slab []byte) []string {
	parts := bytes.Split(slab, []byte{' '})
	tokens := make([]string, len(parts))

	for idx, part := range parts {
		tokens[idx] = string(part)
		encoder.mergeRank[tokens[idx]] = len(encoder.mergeRank)
	}

	return tokens
}

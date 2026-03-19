package provider

import "iter"

/*
RawToken represents a single token sample from a Dataset.
SampleID groups tokens that belong to the same logical sequence.
Symbol is the actual byte value.
Pos is the sequential position of the symbol within the sample.
*/
type RawToken struct {
	SampleID uint32
	Symbol   byte
	Pos      uint32
}

/*
Dataset represents a streaming source of generic token data.
Generate returns an iterator that yields RawTokens sequentially.
*/
type Dataset interface {
	Generate() iter.Seq[RawToken]
}

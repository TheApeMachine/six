package provider

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
Generate returns a channel of RawToken that streams token samples,
and the channel is closed by the Dataset when all tokens have been produced.
*/
type Dataset interface {
	Generate() chan RawToken
}

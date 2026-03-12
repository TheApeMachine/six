package provider

type RawToken struct {
	SampleID uint32
	Symbol   byte
	Pos      uint32
}

type Dataset interface {
	Generate() chan RawToken
}

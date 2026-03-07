package local

import "github.com/theapemachine/six/provider"

type Dataset struct {
	corpus [][]byte
}

func New(corpus [][]byte) *Dataset {
	return &Dataset{corpus: corpus}
}

func (ds *Dataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)
	go func() {
		defer close(out)
		for sampleID, data := range ds.corpus {
			var pos uint32
			for _, b := range data {
				out <- provider.RawToken{
					SampleID: uint32(sampleID),
					Symbol:   b,
					Pos:      pos,
				}
				pos++
			}
		}
	}()
	return out
}

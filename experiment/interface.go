package experiment

import (
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type Result interface {
	Score() float64
}

func GetLoader(dataset provider.Dataset, lsmSpatialIndex float64) *vm.Loader {
	return vm.NewLoader(
		vm.LoaderWithStore(
			store.NewLSMSpatialIndex(lsmSpatialIndex),
		),
		vm.LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(dataset),
			),
		),
	)
}

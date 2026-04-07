package programmer

import "github.com/theapemachine/six/pkg/primitive"

/*
Bundler combines a Value with a program together with
any Assets, which is additional data that is used to
run the program over. It should automatically optimize
the layout for hardware sympathy.
*/
type Bundler struct {
	value  *primitive.Value
	assets [][]uint64
}

func NewBundler(
	value *primitive.Value, assets [][]uint64,
) *Bundler {
	return &Bundler{
		value:  value,
		assets: assets,
	}
}

func (bundler *Bundler) Bundle() *primitive.Value {
	return bundler.value
}

package vm

import (
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Linker buffers incoming Values to provide a sliding window.
It ensures that each Value can be linked to its predecessor and successor.
*/
type Linker struct {
	err    error
	values []*primitive.Value
	lastID uint64
}

/*
linkerOpts configures Linker with options.
*/
type linkerOpts func(*Linker)

/*
NewLinker instantiates a new Linker.
It provides a sliding window over a stream of Values.
*/
func NewLinker(opts ...linkerOpts) *Linker {
	linker := &Linker{
		values: make([]*primitive.Value, 0),
	}

	for _, opt := range opts {
		opt(linker)
	}

	return linker
}

/*
Push adds new Values to the sliding window.
*/
func (linker *Linker) Push(values ...*primitive.Value) {
	for _, value := range values {
		if value == nil {
			continue
		}

		linker.values = append(linker.values, value)
	}
}

/*
Flush forces the remaining Values in the sliding window to be popped.
It should be called when the stream of Values is complete.
*/
func (linker *Linker) Flush() (*primitive.Value, []*programmer.Asset) {
	if len(linker.values) == 0 {
		return nil, nil
	}

	value := linker.values[0]

	assetValue := &primitive.Value{}
	assetStart, _ := primitive.AssetRegion.WordExtent()

	if linker.lastID != 0 {
		assetValue.Set(assetStart, linker.lastID)
	}

	assets := []*programmer.Asset{
		programmer.NewAsset(assetValue, primitive.AssetRegion),
	}

	linker.lastID = value.ID()
	linker.values = linker.values[1:]
	if len(linker.values) == 0 {
		linker.values = nil
	}

	return value, assets
}

/*
Pop returns the next ready Value and its linking assets.
It returns nil if the window does not have enough Values to form a link.
*/
func (linker *Linker) Pop() (*primitive.Value, []*programmer.Asset) {
	if len(linker.values) < 2 {
		return nil, nil
	}

	value := linker.values[0]
	nextValue := linker.values[1]

	assetValue := &primitive.Value{}
	assetStart, _ := primitive.AssetRegion.WordExtent()

	if linker.lastID != 0 {
		assetValue.Set(assetStart, linker.lastID)
	}

	if nextValue != nil {
		assetValue.Set(assetStart+1, nextValue.ID())
	}

	assets := []*programmer.Asset{
		programmer.NewAsset(assetValue, primitive.AssetRegion),
	}

	linker.lastID = value.ID()
	linker.values = linker.values[1:]
	if len(linker.values) == 0 {
		linker.values = nil
	}

	return value, assets
}

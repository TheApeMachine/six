package programmer

import (
	"errors"
	"fmt"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Asset represents any additional data that a Value needs to carry
needed by a program. This is the only way to combine the data
from multiple Values and use it when executing a program.
*/
type Asset struct {
	Value  *primitive.Value
	Region primitive.RegionType
}

/*
NewAsset builds a new Asset from the given Value and Region.
*/
func NewAsset(
	value *primitive.Value,
	region primitive.RegionType,
) *Asset {
	return &Asset{
		Value:  value,
		Region: region,
	}
}

/*
Bundle the Asset into the given Value's Asset region.
*/
func (asset *Asset) Bundle(value *primitive.Value) error {
	if asset.Value == value {
		return errors.New("programmer: Asset.Bundle: cannot bundle a value with itself")
	}

	if asset.Value == nil || value == nil {
		return errors.New("programmer: Asset.Bundle: nil Value")
	}

	dst := value.Get(asset.Region)
	src := asset.Value.Get(asset.Region)

	if dst == nil || src == nil {
		return errors.New("programmer: Asset.Bundle: nil region slice")
	}

	if len(dst) != len(src) {
		return fmt.Errorf(
			"programmer: Asset.Bundle: region word count mismatch (%d vs %d)",
			len(dst),
			len(src),
		)
	}

	copy(dst, src)
	return nil
}

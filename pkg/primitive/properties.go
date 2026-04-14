package primitive

import (
	"errors"

	"github.com/theapemachine/six/pkg/core"
)

type StateType uint64

const (
	PENDING StateType = iota
	READY
	BUSY
	WAITING
	DONE
	RESOLVED
	ERROR
)

type PropertyType uint64

/*
PropertyType indexes words inside the properties region (512 bits = 8 words),
matching DSL properties[k] and pkg/compute/kernel/layout.go absolute words
48+k (e.g. TTL is properties[3] → word 51).
*/
const (
	LABELS PropertyType = iota
	CONFIDENCE
	EPOCH
	TTL
	NOISE
	STATE
	WINDOW
	DEPTH
)

func (value *Value) Property(property PropertyType) (uint64, error) {
	if value == nil {
		return 0, errors.New("value is nil")
	}

	if property > DEPTH {
		return 0, errors.New("property out of range")
	}

	start := core.Cfg.Value.Region.Properties.Start

	return (*value)[start+int(property)], nil
}

package primitive

import (
	"errors"

	"github.com/theapemachine/six/pkg/core"
)

type StatusType uint64

const (
	PENDING StatusType = iota
	READY
	BUSY
	WAITING
	DONE
	RESOLVED
	ERROR
)

type PropertyType int

/*
PropertyType indexes words inside the properties region (first words align with
kernel TTL, community, status, etc.). Role + TargetID live in the extension
band (addressability); see Value.Write and ValueRole.
*/
const (
	LABELS PropertyType = iota
	CONFIDENCE
	EPOCH
	TTL
	NOISE
	STATUS
	WINDOW
	DEPTH
	COMMUNITY
	TARGET
	ROLE
)

/*
ValueRole is the in-band role carried in the Role property word. Zero means no
special role; other values extend the substrate without new property slots.
*/
type ValueRole uint64

const (
	ValueRoleNone ValueRole = iota
	ValueRoleProgrammer
)

func (value *Value) Property(property PropertyType) (uint64, error) {
	if value == nil {
		return 0, errors.New("value is nil")
	}

	if property > ROLE {
		return 0, errors.New("property out of range")
	}

	start := core.Cfg.Value.Region.Properties.Start

	return (*value)[start+int(property)], nil
}

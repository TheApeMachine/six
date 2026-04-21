package primitive

import (
	"encoding/binary"
	"errors"
	"io"

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
	REFERENCE
	EMIT
	SURPRISAL
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

	if property > EMIT {
		return 0, errors.New("property out of range")
	}

	start := core.Cfg.Value.Region.Properties.Start

	return (*value)[start+int(property)], nil
}

/*
StatusWordFromWireFrame returns the STATUS property word from a full wire
frame without materializing a Value. Layout matches Value.Read /
LoadFullFrame: word i occupies bytes [i*8, i*8+8) little-endian — the same
bytes copy() uses on LE; on BE builds valueToPortable still emits LE per
word, so this read stays consistent with the wire.

Cost is one bounds check and an 8-byte load (not JSON, not a full frame copy).
*/
func StatusWordFromWireFrame(frame []byte) (uint64, error) {
	if len(frame) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	word := core.Cfg.Value.Region.Properties.Start + int(STATUS)
	off := word * 8

	if off+8 > len(frame) {
		return 0, io.ErrShortBuffer
	}

	return binary.LittleEndian.Uint64(frame[off : off+8]), nil
}

/*
RequestEmit raises the emit flag in the EMIT property word. The post-ALU
hook installed on the queue picks it up after Dispatch returns and pushes
the resulting wire frame onto the orchestrator's outbound ring so the
frame "falls out" of vm.Orchestrator.Cycle.
*/
func (value *Value) RequestEmit() {
	if value == nil {
		return
	}

	value.Set(core.Cfg.Value.Region.Properties.Start+int(EMIT), 1)
}

/*
EmitRequested reports whether the EMIT property word is non-zero. Pure
read; safe to call from any goroutine because the underlying word is
written via Set (single-word store, atomic on the architectures we
target).
*/
func (value *Value) EmitRequested() bool {
	if value == nil {
		return false
	}

	return (*value)[core.Cfg.Value.Region.Properties.Start+int(EMIT)] != 0
}

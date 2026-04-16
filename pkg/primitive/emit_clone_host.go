package primitive

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
)

const affinityLaneWords = 5

/*
EmitCloneHost mirrors the GPU EMIT_CLONE path for CPU substrates: allocate a
child in the arena, copy the parent frame, XOR-perturb affinity with
properties noise, and stamp a fresh ID.
*/
func EmitCloneHost(parent *Value) *Value {
	if parent == nil {
		return nil
	}

	parentIdx, ok := ArenaIndex(parent)
	if !ok {
		return nil
	}

	child := AllocValue()
	if child == nil {
		return nil
	}

	*child = *parent

	noise := (*parent)[kernel.PropertiesNoiseWord]

	for word := 0; word < affinityLaneWords; word++ {
		(*child)[kernel.AffinityStartWord+uint64(word)] ^= noise ^ (uint64(parentIdx) << (word * 13))
	}

	return child.StampNewID()
}

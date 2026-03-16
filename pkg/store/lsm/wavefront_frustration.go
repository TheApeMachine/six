package lsm

import (
	"sort"

	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

type frustrationSource struct {
	head    *WavefrontHead
	rewound bool
}

type frustrationCandidateKey struct {
	phase     numeric.Phase
	pos       uint32
	segment   uint32
	promptIdx int
	pathLen   int
}

func (wf *Wavefront) frustrateHead(head *WavefrontHead) []*WavefrontHead {
	if head == nil || wf.fe == nil || wf.target == 0 || wf.frustrationForks <= 0 {
		return nil
	}
	if wf.frustrationRounds > 0 && int(head.frustration) >= wf.frustrationRounds {
		return nil
	}

	sources := []frustrationSource{{head: cloneWavefrontHead(head), rewound: false}}
	sources = append(sources, wf.frustrationCheckpointSources(head)...)

	bestByKey := make(map[frustrationCandidateKey]*WavefrontHead)
	candidateBudget := wf.frustrationForks
	if candidateBudget < 1 {
		candidateBudget = 1
	}

	for _, source := range sources {
		if source.head == nil || candidateBudget <= 0 {
			continue
		}

		paths, err := wf.fe.ResolveCandidates(source.head.phase, wf.target, wf.frustrationAttempts, candidateBudget*2)
		if err != nil || len(paths) == 0 {
			continue
		}

		for _, path := range paths {
			candidate := wf.synthesizeFrustrationHead(source.head, head, path, source.rewound)
			if candidate == nil {
				continue
			}

			key := frustrationCandidateKey{
				phase:     candidate.phase,
				pos:       candidate.pos,
				segment:   candidate.segment,
				promptIdx: candidate.promptIdx,
				pathLen:   len(candidate.path),
			}
			if existing, ok := bestByKey[key]; !ok || candidate.energy < existing.energy {
				bestByKey[key] = candidate
			}
		}
	}

	out := make([]*WavefrontHead, 0, len(bestByKey))
	for _, candidate := range bestByKey {
		out = append(out, candidate)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].energy != out[j].energy {
			return out[i].energy < out[j].energy
		}
		if out[i].segment != out[j].segment {
			return out[i].segment < out[j].segment
		}
		if out[i].pos != out[j].pos {
			return out[i].pos < out[j].pos
		}
		return len(out[i].path) < len(out[j].path)
	})

	if len(out) > wf.frustrationForks {
		out = out[:wf.frustrationForks]
	}

	return out
}

func (wf *Wavefront) frustrationCheckpointSources(head *WavefrontHead) []frustrationSource {
	if head == nil || head.registers == nil || wf.frustrationCheckpointFanout <= 0 {
		return nil
	}

	checkpoints := head.registers.RecentCheckpointsBefore(head, wf.frustrationCheckpointFanout)
	out := make([]frustrationSource, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		rewound := rewindHeadFromCheckpoint(head, checkpoint)
		if rewound == nil {
			continue
		}

		rewound.fuzzyErrs = head.fuzzyErrs
		rewound.age = head.age + 1
		rewound.stalls = 0
		rewound.frustration = head.frustration
		rewound.registers = cloneExecutionRegisters(head.registers)
		if rewound.registers == nil {
			rewound.registers = wf.newHeadRegisters()
		}
		rewound.registers.RecordCheckpoint(rewound, checkpointReasonFrustration)
		out = append(out, frustrationSource{head: rewound, rewound: true})
	}

	return out
}

func (wf *Wavefront) stallHead(head *WavefrontHead) *WavefrontHead {
	stalled := cloneWavefrontHead(head)
	if stalled == nil {
		return nil
	}
	if stalled.frustration < 0xFF {
		stalled.frustration++
	}
	stalled.age++
	if stalled.stalls < 0xFF {
		stalled.stalls++
	}
	stalled.energy += 4 + int(stalled.frustration)*4
	if stalled.registers == nil {
		stalled.registers = wf.newHeadRegisters()
	}
	stalled.registers.RecordCheckpoint(stalled, checkpointReasonFrustration)
	return stalled
}

func (wf *Wavefront) synthesizeFrustrationHead(
	source *WavefrontHead,
	original *WavefrontHead,
	opcodes []*macro.MacroOpcode,
	rewound bool,
) *WavefrontHead {
	if source == nil || len(opcodes) == 0 {
		return nil
	}

	phase := source.phase
	pos := source.pos
	path := cloneChordSlice(source.path)
	metaPath := cloneChordSlice(source.metaPath)

	for idx, opcode := range opcodes {
		if opcode == nil || opcode.Rotation == 0 {
			continue
		}

		previous := phase
		phase = wf.calc.Multiply(phase, opcode.Rotation)
		pos++

		value := data.NeutralValue()
		value.SetStatePhase(phase)
		value.SetAffine(opcode.Rotation, 0)
		value.SetTrajectory(previous, phase)
		value.SetGuardRadius(2)
		value.SetMutable(true)

		remaining := len(opcodes) - idx - 1
		program := data.OpcodeNext
		if remaining > 0 {
			program = data.OpcodeBranch
		}
		value.SetProgram(program, 1, uint8(remaining), false)
		path = append(path, value)

		meta := data.MustNewChord()
		meta.SetProgram(data.OpcodeBranch, 1, uint8(remaining), false)
		metaPath = append(metaPath, meta)
	}

	candidate := &WavefrontHead{
		phase:        phase,
		alignedPhase: source.alignedPhase,
		queryPhase:   source.queryPhase,
		pos:          pos,
		segment:      source.segment,
		promptIdx:    source.promptIdx,
		energy:       source.energy + wf.frustrationPenalty(source, original, opcodes, rewound),
		path:         path,
		metaPath:     metaPath,
		visited:      cloneVisitedMap(source.visited),
		fuzzyErrs:    source.fuzzyErrs,
		age:          source.age + 1,
		stalls:       0,
		frustration:  0,
		registers:    cloneExecutionRegisters(source.registers),
	}
	if candidate.registers == nil {
		candidate.registers = wf.newHeadRegisters()
	}

	queryPhase := candidate.queryPhase
	if queryPhase == 0 {
		switch {
		case candidate.alignedPhase != 0:
			queryPhase = candidate.alignedPhase
		default:
			queryPhase = candidate.phase
		}
	}
	candidate.energy += candidate.registers.ObserveTransition(source.phase, queryPhase, candidate.phase)
	candidate.registers.RecordCheckpoint(candidate, checkpointReasonFrustrationFork)
	return candidate
}

func (wf *Wavefront) frustrationPenalty(
	source *WavefrontHead,
	original *WavefrontHead,
	opcodes []*macro.MacroOpcode,
	rewound bool,
) int {
	penalty := 8 + len(opcodes)*6
	if original != nil {
		penalty += int(original.frustration) * 4
	}
	if rewound && source != nil && original != nil {
		posDelta := int(original.pos) - int(source.pos)
		if posDelta < 0 {
			posDelta = -posDelta
		}
		promptDelta := original.promptIdx - source.promptIdx
		if promptDelta < 0 {
			promptDelta = -promptDelta
		}
		penalty += posDelta*3 + promptDelta*4
	}

	supportBonus := 0
	for _, opcode := range opcodes {
		if opcode == nil {
			continue
		}
		if opcode.Hardened {
			supportBonus++
		}
		use := int(opcode.UseCount)
		if use > 3 {
			use = 3
		}
		supportBonus += use
	}
	penalty -= supportBonus

	minimum := len(opcodes)*2 + 4
	if penalty < minimum {
		penalty = minimum
	}
	return penalty
}

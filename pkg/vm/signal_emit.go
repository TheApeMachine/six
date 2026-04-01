package vm

import (
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
minSignalSpanBits is the smallest contiguous span (in token-region bits) that
warrants a dedicated Structure emission. Shorter runs are treated as noise.
*/
const minSignalSpanBits = 32

func tokenRegionParams() (nWords int, base int, totalBits int) {
	totalBits = int(core.Cfg.Value.Region.Tokens.Bits)
	nWords = (totalBits + 63) / 64
	base = core.Cfg.Value.Region.Tokens.Start

	return
}

func tokenSliceFromValue(v *primitive.Value) []uint64 {
	if v == nil {
		return nil
	}

	nWords, base, _ := tokenRegionParams()
	out := make([]uint64, nWords)

	for i := 0; i < nWords; i++ {
		idx := base + i
		if idx >= 0 && idx < primitive.Words {
			out[i] = v[idx]
		}
	}

	return out
}

/*
newSpanStructure builds a Structure whose token region carries bits
[startBit, startBit+spanBits) copied from wordSrc (parent A or AND row),
with Prev linked to parent. workspace is the post-learn frame used for HIE.
*/
func newSpanStructure(parent, workspace *primitive.Value, kind StructureKind, wordSrc []uint64, startBit, spanBits int) (Structure, bool) {
	if parent == nil || workspace == nil || len(wordSrc) == 0 {
		return Structure{}, false
	}

	if spanBits < minSignalSpanBits {
		return Structure{}, false
	}

	nWords, base, totalBits := tokenRegionParams()

	if startBit < 0 || spanBits <= 0 || startBit+spanBits > totalBits {
		return Structure{}, false
	}

	packed := primitive.ExtractSpan(wordSrc, startBit, spanBits)

	var frame primitive.Value

	primitive.CopyFrame(&frame, parent)

	frame[core.Cfg.Value.Region.ID.Start] = nextStructureFrameID()
	frame[core.Cfg.Value.Region.Prev.Start] = parent[core.Cfg.Value.Region.ID.Start]
	frame[core.Cfg.Value.Region.Next.Start] = 0

	for i := 0; i < nWords; i++ {
		idx := base + i
		if idx >= 0 && idx < primitive.Words {
			frame[idx] = 0
		}
	}

	for i := 0; i < len(packed); i++ {
		idx := base + i
		if idx >= 0 && idx < primitive.Words {
			frame[idx] = packed[i]
		}
	}

	applyHolographicBlend(parent, workspace, &frame)

	return Structure{
		Kind:          kind,
		SourceValueID: parent[core.Cfg.Value.Region.ID.Start],
		Frame:         frame,
		ExecExit:      uint16(frame[primitive.ExecStatusWord] >> primitive.ExecStatusShift),
	}, true
}

/*
EmitFromPairwiseSignals materializes README “cancel / merge” cuts from ScanSignals
over canonical parentA and partnerB. workspace must be the post-learn self frame
(the in-band XOR cancel map) so HIE sees the same bias signal as StructureFromWorkspace.
Shorter ScanSignals hits are retained in exchange; only the decisive local spans
per kind drive emission here, expanded into shared + left + right residues for cancel.
*/
func EmitFromPairwiseSignals(parentA, partnerB, workspace *primitive.Value) []Structure {
	if parentA == nil || partnerB == nil || workspace == nil {
		return nil
	}

	nWords, base, totalBits := tokenRegionParams()
	signals := primitive.ScanSignals(parentA, partnerB, nWords, base)
	local, _ := primitive.SplitSignals(signals)

	wordsA := tokenSliceFromValue(parentA)
	wordsB := tokenSliceFromValue(partnerB)

	var out []Structure

	for _, sig := range local {
		switch sig.Kind {
		case primitive.SignalCancel:
			if st, ok := newSpanStructure(parentA, workspace, StructureKindLearnCancel, wordsA, sig.StartBit, sig.Length); ok {
				out = append(out, st)
			}

			if sig.StartBit >= minSignalSpanBits {
				if st, ok := newSpanStructure(parentA, workspace, StructureKindLearnCancel, wordsA, 0, sig.StartBit); ok {
					out = append(out, st)
				}
			}

			rightLen := totalBits - sig.StartBit - sig.Length
			if rightLen >= minSignalSpanBits {
				if st, ok := newSpanStructure(parentA, workspace, StructureKindLearnCancel, wordsA, sig.StartBit+sig.Length, rightLen); ok {
					out = append(out, st)
				}
			}

		case primitive.SignalMerge:
			andW := make([]uint64, len(wordsA))
			for i := range wordsA {
				if i < len(wordsB) {
					andW[i] = wordsA[i] & wordsB[i]
				}
			}

			if st, ok := newSpanStructure(parentA, workspace, StructureKindBuildMerge, andW, sig.StartBit, sig.Length); ok {
				out = append(out, st)
			}
		}
	}

	return out
}

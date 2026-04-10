package kadabra

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

const directSequenceCompletionScore = 128.0

const sequenceSurfaceShardCount = 1024

const sequenceSurfaceMinOverlap = 6

/*
sequenceSurfaceMap is an immutable ValueID-to-surface snapshot.
It stores only short segment strings, so clone cost is bounded by one
shard instead of the full Kadabra ingest history.
*/
type sequenceSurfaceMap struct {
	m map[uint64]string
}

/*
sequenceSurfaceShard owns one copy-on-write surface map.
The zero value is ready, which avoids allocating empty maps for nodes
that never need cross-segment surface completion.
*/
type sequenceSurfaceShard struct {
	ptr atomic.Pointer[sequenceSurfaceMap]
}

/*
sequenceSurfaceIndex keeps enough byte-level sequence memory for a
node to continue a prompt that ends inside one Value into the next
linked Value. This directly targets holdout completions that cross the
fixed-size primitive.Value segment boundary.
*/
type sequenceSurfaceIndex struct {
	byID       [sequenceSurfaceShardCount]sequenceSurfaceShard
	nextByPrev [sequenceSurfaceShardCount]sequenceSurfaceShard
}

func (node *Node) rememberSequenceSurface(record SequenceRecord) {
	if node == nil {
		return
	}

	id := record.Value.ID()
	surface := record.Value.String()

	if id == 0 || surface == "" {
		return
	}

	prevID := record.Value[core.Cfg.Value.Region.Prev.Start]

	node.sequenceSurfaces.Store(id, prevID, surface)
}

func (node *Node) partialSequenceContinuations(
	value *primitive.Value,
) []algo.Continuation {
	if node == nil || value == nil {
		return nil
	}

	querySurface := value.String()

	if querySurface == "" {
		return nil
	}

	return node.sequenceSurfaces.Continuations(node.ID, querySurface)
}

/*
Store records the segment surface and, when present, the previous segment
link used to bridge a completion across primitive.Value boundaries.
*/
func (index *sequenceSurfaceIndex) Store(
	id uint64,
	prevID uint64,
	surface string,
) {
	if index == nil || id == 0 || surface == "" {
		return
	}

	index.byID[sequenceSurfaceShardIndex(id)].Store(id, surface)

	if prevID != 0 {
		index.nextByPrev[sequenceSurfaceShardIndex(prevID)].Store(prevID, surface)
	}
}

/*
Continuations returns the strongest direct surface completions for a
prompt prefix. Query-time scanning is intentionally read-only: each map
snapshot is immutable after publication.
*/
func (index *sequenceSurfaceIndex) Continuations(
	origin uint64,
	querySurface string,
) []algo.Continuation {
	if index == nil || querySurface == "" {
		return nil
	}

	limit := core.Cfg.MarkovTrie.BeamWidth

	if limit <= 0 {
		limit = 3
	}

	candidates := make([]algo.Continuation, 0, limit)

	for idx := range index.byID {
		snapshot := index.byID[idx].ptr.Load()

		if snapshot == nil || len(snapshot.m) == 0 {
			continue
		}

		for id, surface := range snapshot.m {
			overlap := sequenceSurfaceOverlap(querySurface, surface)

			if overlap == 0 {
				continue
			}

			suffix := surface[overlap:]
			nextSurface := index.Next(id)

			if suffix != "" {
				candidates = appendSequenceContinuation(
					candidates,
					suffix,
					origin,
					limit,
					overlap,
				)
			}

			if nextSurface != "" {
				candidates = appendSequenceContinuation(
					candidates,
					suffix+nextSurface,
					origin,
					limit,
					overlap,
				)
			}
		}
	}

	return candidates
}

/*
Next returns the surface segment linked after id.
*/
func (index *sequenceSurfaceIndex) Next(id uint64) string {
	if index == nil || id == 0 {
		return ""
	}

	return index.nextByPrev[sequenceSurfaceShardIndex(id)].Load(id)
}

/*
Store swaps a shard snapshot with a single-key mutation.
*/
func (shard *sequenceSurfaceShard) Store(key uint64, surface string) {
	if shard == nil || key == 0 || surface == "" {
		return
	}

	for {
		old := shard.ptr.Load()

		if old != nil && old.m != nil && old.m[key] == surface {
			return
		}

		base := make(map[uint64]string, 1)

		if old != nil && old.m != nil {
			base = make(map[uint64]string, len(old.m)+1)

			maps.Copy(base, old.m)
		}

		base[key] = surface
		next := &sequenceSurfaceMap{m: base}

		if shard.ptr.CompareAndSwap(old, next) {
			return
		}
	}
}

/*
Load returns a published surface from a shard snapshot.
*/
func (shard *sequenceSurfaceShard) Load(key uint64) string {
	if shard == nil || key == 0 {
		return ""
	}

	snapshot := shard.ptr.Load()

	if snapshot == nil {
		return ""
	}

	return snapshot.m[key]
}

func sequenceSurfaceShardIndex(key uint64) int {
	return int(key) & (sequenceSurfaceShardCount - 1)
}

func appendSequenceContinuation(
	candidates []algo.Continuation,
	candidate string,
	origin uint64,
	limit int,
	overlap int,
) []algo.Continuation {
	candidate = trimSequenceContinuation(candidate)

	if candidate == "" {
		return candidates
	}

	score := directSequenceCompletionScore
	score += float64(overlap) / float64(max(
		1,
		core.Cfg.Value.Region.MaxTokenIngestBytes(),
	))
	score += float64(len(candidate)) / float64(max(
		1,
		core.Cfg.Value.Region.MaxTokenIngestBytes(),
	))

	next := algo.Continuation{
		Sequence: []byte(candidate),
		Score:    score,
		Origin:   origin,
	}

	for idx := range candidates {
		if string(candidates[idx].Sequence) == candidate {
			if next.Score > candidates[idx].Score {
				candidates[idx] = next
				sortSequenceContinuations(candidates)
			}

			return candidates
		}
	}

	if len(candidates) < limit {
		candidates = append(candidates, next)
		sortSequenceContinuations(candidates)

		return candidates
	}

	if compareSequenceContinuation(next, candidates[len(candidates)-1]) >= 0 {
		return candidates
	}

	candidates[len(candidates)-1] = next
	sortSequenceContinuations(candidates)

	return candidates
}

func trimSequenceContinuation(candidate string) string {
	limit := core.Cfg.Value.Region.MaxTokenIngestBytes()

	if limit > 0 && len(candidate) > limit {
		return candidate[:limit]
	}

	return candidate
}

func sequenceSurfaceOverlap(querySurface string, surface string) int {
	limit := min(len(querySurface), len(surface))

	if limit == 0 {
		return 0
	}

	minOverlap := min(sequenceSurfaceMinOverlap, limit)

	for overlap := limit; overlap >= minOverlap; overlap-- {
		querySuffix := querySurface[len(querySurface)-overlap:]

		if strings.TrimSpace(querySuffix) == "" {
			continue
		}

		if querySuffix == surface[:overlap] {
			return overlap
		}
	}

	return 0
}

func sortSequenceContinuations(candidates []algo.Continuation) {
	slices.SortStableFunc(candidates, compareSequenceContinuation)
}

func compareSequenceContinuation(
	left algo.Continuation,
	right algo.Continuation,
) int {
	if left.Score != right.Score {
		return cmp.Compare(right.Score, left.Score)
	}

	return cmp.Compare(string(left.Sequence), string(right.Sequence))
}

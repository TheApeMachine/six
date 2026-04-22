package mesh

import (
	"io"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/primitive"
)

const maxLearnersPerRefresh = 4

func (field *Field) spawnUnsupervisedLearnerConn(members []*primitive.Value, metric FieldMetrics) {
	if field == nil || field.conn == nil || field.queue == nil || len(members) < 2 {
		return
	}

	// Bound learner creation so each low-coverage field gets a small swarm,
	// not an unbounded new swarm on every metric tick.
	if field.learnerEmissions.Load() >= uint64(metric.MemberCount) {
		return
	}

	limit := min(maxLearnersPerRefresh, len(members))
	components := make([]io.ReadWriter, 0, limit)

	for i := 0; i < len(members) && len(components) < limit; i++ {
		member := members[i]
		if member == nil {
			continue
		}

		learner := primitive.Emit(
			primitive.WithFirmware(core.UNSUPERVISED_LEARN),
			primitive.WithRole(uint64(primitive.ValueRoleLearner)),
			primitive.WithTarget(field.id),
			primitive.WithCommunity(field.id),
			primitive.WithTTL(2),
		)

		seedLearnerFromMember(learner, member)
		components = append(components, learner)
	}

	if len(components) == 0 {
		return
	}

	learnConn, err := gossip.NewConn(field.ctx, field.queue, field.telemetry, io.Discard, components...)
	if err != nil {
		return
	}

	field.learnerEmissions.Add(uint64(len(components)))
	field.conn.Update(learnConn)
}

func seedLearnerFromMember(learner, member *primitive.Value) {
	if learner == nil || member == nil {
		return
	}

	copy(learner.Get(primitive.ContextRegion), member.Get(primitive.ContextRegion))
	copy(learner.Get(primitive.GradientRegion), member.Get(primitive.GradientRegion))
	copy(learner.Get(primitive.AffinityRegion), member.Get(primitive.AffinityRegion))

	// Many raw ingested Values have not yet formed context. Give learners a
	// structural starting point by mirroring the first token words into context.
	ctx := learner.Get(primitive.ContextRegion)
	empty := true
	for _, word := range ctx {
		if word != 0 {
			empty = false
			break
		}
	}

	if empty {
		tokens := member.Get(primitive.TokenRegion)
		for i := 0; i < len(ctx) && i < len(tokens); i++ {
			ctx[i] = tokens[i]
		}
	}

	learner.NormalizeAffinity()
}

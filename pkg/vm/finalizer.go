package vm

import (
	"strings"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
ActionFinalizer interprets generic config-driven post-ALU rules. It never
recomputes algorithm semantics in Go; it only copies already-written in-band
state, optionally reprograms the current Value, or emits an ephemeral clone
that will execute a named config program.
*/
type ActionFinalizer struct {
	orchestrator *Orchestrator
}

const (
	valueFinalizerScope     = "value"
	communityFinalizerScope = "community"
	globalFinalizerScope    = "global"
)

type actionContext struct {
	scope       string
	value       *primitive.Value
	field       *geometry.Field
	communities int
}

/*
NewActionFinalizer binds the generic post-ALU runtime to one orchestrator.
*/
func NewActionFinalizer(orchestrator *Orchestrator) *ActionFinalizer {
	return &ActionFinalizer{orchestrator: orchestrator}
}

/*
FinalizeValue runs value-scope rules immediately after one ALU pass. It returns
true when one of those rules reprogrammed the current Value and consumed normal
re-entry into the orchestrator.
*/
func (finalizer *ActionFinalizer) FinalizeValue(value *primitive.Value) bool {
	context := actionContext{
		scope: valueFinalizerScope,
		value: value,
	}

	return finalizer.apply(context)
}

/*
FinalizeField runs community/global rules after a routed Value has updated the
local field state.
*/
func (finalizer *ActionFinalizer) FinalizeField(
	scope string,
	value *primitive.Value,
	field *geometry.Field,
) bool {
	context := actionContext{
		scope:       scope,
		value:       value,
		field:       field,
		communities: finalizer.communityCount(),
	}

	return finalizer.apply(context)
}

func (finalizer *ActionFinalizer) apply(context actionContext) bool {
	if finalizer == nil || finalizer.orchestrator == nil || context.value == nil {
		return false
	}

	consumed := false

	for _, rule := range core.Cfg.Finalizers {
		if !finalizer.matches(rule, context) {
			continue
		}

		for _, action := range rule.Actions {
			if finalizer.runAction(action, context) {
				consumed = true
			}
		}
	}

	return consumed
}

func (finalizer *ActionFinalizer) matches(
	rule core.FinalizerRuleConfig,
	context actionContext,
) bool {
	if context.value == nil {
		return false
	}

	scope := strings.ToLower(strings.TrimSpace(rule.Scope))
	if scope == "" {
		scope = valueFinalizerScope
	}

	if scope != context.scope {
		return false
	}

	if !finalizer.matchRegions(rule.Regions, context.value) {
		return false
	}

	if rule.MinMembers > 0 {
		if context.field == nil || len(context.field.Values) < rule.MinMembers {
			return false
		}
	}

	if rule.MinCommunities > 0 && context.communities < rule.MinCommunities {
		return false
	}

	if rule.MinConcentration > 0 {
		if context.field == nil {
			return false
		}

		if context.field.Dominant().Concentration < rule.MinConcentration {
			return false
		}
	}

	return true
}

func (finalizer *ActionFinalizer) matchRegions(
	regions map[string]bool,
	value *primitive.Value,
) bool {
	if len(regions) == 0 {
		return true
	}

	firmware := finalizer.orchestrator.firmware
	if firmware == nil {
		firmware = programmer.NewFirmware()
	}

	for rawName, want := range regions {
		regionType, ok := primitive.RegionNames[strings.ToLower(strings.TrimSpace(rawName))]
		if !ok {
			return false
		}

		hasBits := firmware.HasBits(value.Get(regionType))
		if hasBits != want {
			return false
		}
	}

	return true
}

func (finalizer *ActionFinalizer) runAction(
	action core.FinalizerActionConfig,
	context actionContext,
) bool {
	switch strings.ToLower(strings.TrimSpace(action.Type)) {
	case "reprogram":
		if strings.TrimSpace(action.Program) == "" {
			return false
		}

		finalizer.reprogram(context.value, action, context)
		return true
	case "emit":
		_ = finalizer.emit(context.value, action, context)
		return false
	}

	return false
}

func (finalizer *ActionFinalizer) reprogram(
	value *primitive.Value,
	action core.FinalizerActionConfig,
	context actionContext,
) {
	if value == nil || strings.TrimSpace(action.Program) == "" {
		return
	}

	finalizer.applyCopies(value, action.Copies, context)
	finalizer.submitExecutable(value, action.Program)
}

func (finalizer *ActionFinalizer) emit(
	source *primitive.Value,
	action core.FinalizerActionConfig,
	context actionContext,
) *primitive.Value {
	if source == nil {
		return nil
	}

	emitted, err := primitive.ValueFromWireFrame(source.Bytes())
	if err != nil {
		return nil
	}

	emitted.StampNewID()
	finalizer.prepareEmission(emitted, source, action, context)

	if strings.TrimSpace(action.Program) != "" {
		finalizer.submitExecutable(emitted, action.Program)
		return emitted
	}

	finalizer.orchestrator.publishPrepared(emitted)
	return emitted
}

func (finalizer *ActionFinalizer) prepareEmission(
	emitted *primitive.Value,
	source *primitive.Value,
	action core.FinalizerActionConfig,
	context actionContext,
) {
	if emitted == nil {
		return
	}

	finalizer.zeroRegion(emitted, primitive.ProgramRegion)
	finalizer.zeroRegion(emitted, primitive.SignalsRegion)
	finalizer.zeroRegion(emitted, primitive.AssetRegion)

	emitted.Set(kernel.SchedulingNextProgramWord, 0)
	emitted.Set(kernel.PrevStartWord, source.ID())
	emitted.Set(kernel.NextStartWord, 0)

	if action.TTL > 0 {
		emitted.Set(kernel.PropertiesTTLWord, action.TTL)
	}

	finalizer.applyCopies(emitted, action.Copies, context)
}

func (finalizer *ActionFinalizer) submitExecutable(
	value *primitive.Value,
	program string,
) {
	if value == nil || finalizer == nil || finalizer.orchestrator == nil {
		return
	}

	executable := programmer.NewExecutable(value, program, nil)
	executable.SetFinalizer(finalizer.orchestrator.finalizeExecutedValue)

	finalizer.orchestrator.queue.Submit(func() *programmer.Executable {
		return executable
	})
}

func (finalizer *ActionFinalizer) applyCopies(
	target *primitive.Value,
	copies []core.FinalizerCopyConfig,
	context actionContext,
) {
	for _, copyConfig := range copies {
		sourceWords := finalizer.resolveSource(copyConfig.Source, context)
		if len(sourceWords) == 0 {
			continue
		}

		destination, err := programmer.ParseRegionRef(copyConfig.Destination)
		if err != nil {
			continue
		}

		destinationWords := valueSpan(target, destination)
		if len(destinationWords) == 0 {
			continue
		}

		clear(destinationWords)
		copy(destinationWords, sourceWords)
	}
}

func (finalizer *ActionFinalizer) resolveSource(
	raw string,
	context actionContext,
) []uint64 {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return nil
	}

	if strings.HasPrefix(text, "field.") {
		return finalizer.fieldSource(strings.TrimPrefix(text, "field."), context.field)
	}

	if strings.HasPrefix(text, "value.") {
		text = strings.TrimPrefix(text, "value.")
	}

	ref, err := programmer.ParseRegionRef(text)
	if err != nil {
		return nil
	}

	return valueSpan(context.value, ref)
}

func (finalizer *ActionFinalizer) fieldSource(
	raw string,
	field *geometry.Field,
) []uint64 {
	if field == nil {
		return nil
	}

	ref, err := programmer.ParseRegionRef(raw)
	if err != nil {
		return nil
	}

	if ref.Region != primitive.AffinityRegion {
		return nil
	}

	start, _ := primitive.AffinityRegion.WordExtent()
	offset := ref.Start - start
	end, ok := spanEnd(offset, ref.Span, len(field.Affinity))
	if !ok {
		return nil
	}

	return field.Affinity[offset:end]
}

func (finalizer *ActionFinalizer) communityCount() int {
	if finalizer == nil || finalizer.orchestrator == nil || finalizer.orchestrator.router == nil {
		return 0
	}

	return finalizer.orchestrator.router.CommunityCount()
}

func (finalizer *ActionFinalizer) zeroRegion(
	value *primitive.Value,
	region primitive.RegionType,
) {
	words := value.Get(region)
	clear(words)
}

func valueSpan(value *primitive.Value, ref programmer.RegionRef) []uint64 {
	if value == nil {
		return nil
	}

	end, ok := spanEnd(ref.Start, ref.Span, len(*value))
	if !ok {
		return nil
	}

	return (*value)[ref.Start:end]
}

func spanEnd(start int, span int, limit int) (int, bool) {
	if start < 0 || span < 0 || start > limit {
		return 0, false
	}

	if span > limit-start {
		return 0, false
	}

	return start + span, true
}

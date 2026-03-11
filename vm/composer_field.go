package vm

import (
	"math"
	"sort"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/resonance"
	"github.com/theapemachine/six/vm/cortex"
)

type pairPriorKey struct {
	Left  data.Chord
	Right data.Chord
}

type triplePriorKey struct {
	Left  data.Chord
	Mid   data.Chord
	Right data.Chord
}

type fieldSolution struct {
	Span  []data.Chord
	Score float64
}

type weightedLogicRule struct {
	Rule       cortex.LogicRule
	Activation float64
}

type weightedLogicChain struct {
	Chain      cortex.LogicChain
	Activation float64
}

type weightedLogicCircuit struct {
	Circuit    cortex.LogicCircuit
	Activation float64
}

type logicTemplate struct {
	Sequence   []data.Chord
	Program    data.Chord
	Start      int
	Entry      data.Chord
	Exit       data.Chord
	Activation float64
}

type composerLogicField struct {
	rules       []weightedLogicRule
	chains      []weightedLogicChain
	circuits    []weightedLogicCircuit
	templates   []logicTemplate
	leftAnchor  data.Chord
	rightAnchor data.Chord
	extraHints  []data.Chord
}

type logicChainKey struct {
	LeftInterface  data.Chord
	LeftPayload    data.Chord
	RightInterface data.Chord
	RightPayload   data.Chord
	Program        data.Chord
}

func newComposerLogicField(boundary SpanBoundary) *composerLogicField {
	leftAnchor := lastChord(boundary.Left)
	rightAnchor := firstChord(boundary.Right)
	extraHints := append([]data.Chord(nil), boundary.Hints...)
	extraHints = append(extraHints, boundary.Logic.Signals...)

	field := &composerLogicField{
		leftAnchor:  leftAnchor,
		rightAnchor: rightAnchor,
		extraHints:  extraHints,
		rules:       make([]weightedLogicRule, 0, len(boundary.Logic.Rules)),
		chains:      make([]weightedLogicChain, 0, len(boundary.Logic.Chains)+8),
		circuits:    make([]weightedLogicCircuit, 0, len(boundary.Logic.Circuits)+8),
	}

	for _, rule := range boundary.Logic.Rules {
		if rule.Interface.ActiveCount() == 0 && rule.Payload.ActiveCount() == 0 {
			continue
		}

		activation := math.Log1p(float64(max(rule.Support, 1)))
		activation *= roleScale(rule.Role)

		if leftAnchor.ActiveCount() > 0 {
			activation += logicOverlap(leftAnchor, rule.Interface, rule.Program) * 1.35
		}

		if rightAnchor.ActiveCount() > 0 {
			activation += logicOverlap(rightAnchor, rule.Payload, rule.Program) * 1.35
		}

		for _, hint := range extraHints {
			if hint.ActiveCount() == 0 {
				continue
			}
			activation += logicOverlap(hint, rule.Interface, rule.Program) * 0.45
			activation += logicOverlap(hint, rule.Payload, rule.Program) * 0.15
		}

		field.rules = append(field.rules, weightedLogicRule{
			Rule:       rule,
			Activation: activation,
		})
	}

	chainSeen := make(map[logicChainKey]struct{})
	appendChain := func(chain cortex.LogicChain) {
		if chain.Left.Interface.ActiveCount() == 0 && chain.Right.Payload.ActiveCount() == 0 {
			return
		}

		if chain.Program.ActiveCount() == 0 {
			chain.Program = data.ChordOR(&chain.Left.Program, &chain.Right.Program)
		}
		if chain.Bridge.ActiveCount() == 0 {
			chain.Bridge = data.ChordAND(&chain.Left.Payload, &chain.Right.Interface)
		}
		if chain.Bridge.ActiveCount() == 0 {
			chain.Bridge = data.ChordOR(&chain.Left.Payload, &chain.Right.Interface)
		}
		if chain.Support <= 0 {
			chain.Support = min(chain.Left.Support, chain.Right.Support)
		}
		if chain.Support <= 0 {
			return
		}

		key := logicChainKey{
			LeftInterface:  chain.Left.Interface,
			LeftPayload:    chain.Left.Payload,
			RightInterface: chain.Right.Interface,
			RightPayload:   chain.Right.Payload,
			Program:        chain.Program,
		}
		if _, ok := chainSeen[key]; ok {
			return
		}
		chainSeen[key] = struct{}{}

		activation := math.Log1p(float64(max(chain.Support, 1)))
		bridgeWeight := resonance.OverlapScore(&chain.Left.Payload, &chain.Right.Interface)
		activation += bridgeWeight * 0.9

		if leftAnchor.ActiveCount() > 0 {
			activation += logicOverlap(leftAnchor, chain.Left.Interface, chain.Program) * 1.2
		}
		if rightAnchor.ActiveCount() > 0 {
			activation += logicOverlap(rightAnchor, chain.Right.Payload, chain.Program) * 1.2
		}
		for _, hint := range extraHints {
			if hint.ActiveCount() == 0 {
				continue
			}
			activation += logicOverlap(hint, chain.Left.Interface, chain.Program) * 0.3
			activation += logicOverlap(hint, chain.Right.Payload, chain.Program) * 0.2
		}

		field.chains = append(field.chains, weightedLogicChain{
			Chain:      chain,
			Activation: activation,
		})
	}

	for _, chain := range boundary.Logic.Chains {
		appendChain(chain)
	}

	for _, chain := range inferImplicitChains(field.rules) {
		appendChain(chain)
	}

	circuitSeen := make(map[string]struct{})
	appendCircuit := func(circuit cortex.LogicCircuit) {
		if circuit.Len() < 3 {
			return
		}

		payloads := circuit.Payloads()
		if len(payloads) < 3 {
			return
		}

		if circuit.Program.ActiveCount() == 0 {
			for _, step := range circuit.Steps {
				if step.Program.ActiveCount() == 0 {
					continue
				}
				if circuit.Program.ActiveCount() == 0 {
					circuit.Program = step.Program
					continue
				}
				circuit.Program = data.ChordOR(&circuit.Program, &step.Program)
			}
		}
		if circuit.Support <= 0 {
			for _, step := range circuit.Steps {
				if step.Support <= 0 {
					continue
				}
				if circuit.Support == 0 {
					circuit.Support = step.Support
					continue
				}
				circuit.Support = min(circuit.Support, step.Support)
			}
		}
		if circuit.Support <= 0 {
			return
		}

		activation := circuit.Weight()
		if leftAnchor.ActiveCount() > 0 {
			activation += logicOverlap(leftAnchor, circuit.Entry(), circuit.Program) * 1.4
		}
		if rightAnchor.ActiveCount() > 0 {
			activation += logicOverlap(rightAnchor, circuit.Exit(), circuit.Program) * 1.25
		}
		for _, payload := range payloads {
			for _, hint := range extraHints {
				if hint.ActiveCount() == 0 {
					continue
				}
				activation += resonance.OverlapScore(&hint, &payload) * 0.12
			}
		}

		key := circuit.Signature()
		if key == "" {
			return
		}
		if _, ok := circuitSeen[key]; ok {
			return
		}
		circuitSeen[key] = struct{}{}

		field.circuits = append(field.circuits, weightedLogicCircuit{
			Circuit:    circuit,
			Activation: activation,
		})
	}

	for _, circuit := range boundary.Logic.Circuits {
		appendCircuit(circuit)
	}

	for _, circuit := range inferImplicitCircuits(field.chains) {
		appendCircuit(circuit)
	}

	if len(field.rules) == 0 && len(field.chains) == 0 && len(field.circuits) == 0 {
		return nil
	}

	sort.Slice(field.rules, func(i, j int) bool {
		if field.rules[i].Activation == field.rules[j].Activation {
			return field.rules[i].Rule.Support > field.rules[j].Rule.Support
		}
		return field.rules[i].Activation > field.rules[j].Activation
	})

	sort.Slice(field.chains, func(i, j int) bool {
		if field.chains[i].Activation == field.chains[j].Activation {
			return field.chains[i].Chain.Support > field.chains[j].Chain.Support
		}
		return field.chains[i].Activation > field.chains[j].Activation
	})

	sort.Slice(field.circuits, func(i, j int) bool {
		if field.circuits[i].Activation == field.circuits[j].Activation {
			return field.circuits[i].Circuit.Support > field.circuits[j].Circuit.Support
		}
		return field.circuits[i].Activation > field.circuits[j].Activation
	})

	if len(field.rules) > 24 {
		field.rules = field.rules[:24]
	}
	if len(field.chains) > 16 {
		field.chains = field.chains[:16]
	}
	if len(field.circuits) > 12 {
		field.circuits = field.circuits[:12]
	}

	field.templates = field.buildTemplates(boundary.Width)

	return field
}

func inferImplicitChains(rules []weightedLogicRule) []cortex.LogicChain {
	if len(rules) < 2 {
		return nil
	}

	limit := min(len(rules), 8)
	chains := make([]cortex.LogicChain, 0, limit)

	for i := 0; i < limit; i++ {
		left := rules[i].Rule
		if left.Payload.ActiveCount() == 0 {
			continue
		}

		for j := 0; j < limit; j++ {
			if i == j {
				continue
			}

			right := rules[j].Rule
			if right.Interface.ActiveCount() == 0 || right.Payload.ActiveCount() == 0 {
				continue
			}

			bridge := data.ChordAND(&left.Payload, &right.Interface)
			overlap := resonance.OverlapScore(&left.Payload, &right.Interface)
			if overlap < 0.45 {
				continue
			}

			program := data.ChordOR(&left.Program, &right.Program)
			support := min(left.Support, right.Support) + int(overlap*4.0)

			chains = append(chains, cortex.LogicChain{
				Left:    left,
				Right:   right,
				Bridge:  bridge,
				Program: program,
				Support: support,
			})
		}
	}

	if len(chains) > 12 {
		chains = chains[:12]
	}

	return chains
}

func inferImplicitCircuits(chains []weightedLogicChain) []cortex.LogicCircuit {
	if len(chains) < 2 {
		return nil
	}

	limit := min(len(chains), 8)
	circuits := make([]cortex.LogicCircuit, 0, limit)
	seen := make(map[string]struct{}, limit)

	for i := 0; i < limit; i++ {
		left := chains[i].Chain
		for j := 0; j < limit; j++ {
			if i == j {
				continue
			}

			right := chains[j].Chain
			overlap := resonance.OverlapScore(&left.Right.Payload, &right.Left.Interface)
			if overlap < 0.45 {
				continue
			}

			program := data.ChordOR(&left.Program, &right.Program)
			support := min(left.Support, right.Support) + int(overlap*5.0)
			if support <= 0 {
				continue
			}

			circuit := cortex.LogicCircuit{
				Steps: []cortex.LogicRule{
					left.Left,
					left.Right,
					right.Right,
				},
				Program: program,
				Support: support,
			}

			key := circuit.Signature()
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			circuits = append(circuits, circuit)
		}
	}

	sort.Slice(circuits, func(i, j int) bool {
		leftWeight := circuits[i].Weight()
		rightWeight := circuits[j].Weight()
		if leftWeight == rightWeight {
			return circuits[i].Len() > circuits[j].Len()
		}
		return leftWeight > rightWeight
	})

	if len(circuits) > 12 {
		circuits = circuits[:12]
	}

	return circuits
}

func (field *composerLogicField) hasCircuits() bool {
	return field != nil && len(field.templates) > 0
}

func (field *composerLogicField) buildTemplates(width int) []logicTemplate {
	if field == nil || width <= 0 || len(field.circuits) == 0 {
		return nil
	}

	templates := make([]logicTemplate, 0, len(field.circuits)*3)
	seen := make(map[string]struct{}, len(field.circuits)*3)

	appendTemplate := func(sequence []data.Chord, start int, entry, exit, program data.Chord, activation float64) {
		if len(sequence) == 0 || start < 0 || start+len(sequence) > width || activation <= 0 {
			return
		}

		buf := make([]byte, 0, len(sequence)*64+4)
		for _, chord := range sequence {
			buf = append(buf, chord.Bytes()...)
		}
		buf = append(buf, byte(start), byte(len(sequence)))
		key := string(buf)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}

		templates = append(templates, logicTemplate{
			Sequence:   append([]data.Chord(nil), sequence...),
			Program:    program,
			Start:      start,
			Entry:      entry,
			Exit:       exit,
			Activation: activation,
		})
	}

	for _, weighted := range field.circuits {
		payloads := weighted.Circuit.Payloads()
		if len(payloads) == 0 {
			continue
		}

		entry := weighted.Circuit.Entry()
		exit := weighted.Circuit.Exit()
		program := weighted.Circuit.Program

		activation := weighted.Activation
		if activation <= 0 {
			activation = weighted.Circuit.Weight()
		}

		leftFit := 0.0
		if field.leftAnchor.ActiveCount() > 0 {
			leftFit = logicOverlap(field.leftAnchor, entry, program)
		}
		rightFit := 0.0
		if field.rightAnchor.ActiveCount() > 0 {
			rightFit = logicOverlap(field.rightAnchor, exit, program)
		}

		if len(payloads) <= width {
			starts := []int{0}
			endStart := width - len(payloads)
			if endStart != 0 {
				starts = append(starts, endStart)
			}
			centerStart := (width - len(payloads)) / 2
			if centerStart != 0 && centerStart != endStart {
				starts = append(starts, centerStart)
			}

			for _, start := range starts {
				templateActivation := activation * 0.7
				if start == 0 {
					templateActivation += activation * leftFit
				}
				if start+len(payloads) == width {
					templateActivation += activation * rightFit
				}
				appendTemplate(payloads, start, entry, exit, program, templateActivation)
			}
			continue
		}

		prefix := payloads[:width]
		prefixExit := exit
		if len(payloads) > width {
			prefixExit = payloads[width]
		}
		appendTemplate(prefix, 0, entry, prefixExit, program, activation*(0.75+leftFit))

		suffixStart := len(payloads) - width
		suffix := payloads[suffixStart:]
		suffixEntry := entry
		if suffixStart > 0 {
			suffixEntry = payloads[suffixStart-1]
		}
		appendTemplate(suffix, 0, suffixEntry, exit, program, activation*(0.75+rightFit))
	}

	sort.Slice(templates, func(i, j int) bool {
		if templates[i].Activation == templates[j].Activation {
			return len(templates[i].Sequence) > len(templates[j].Sequence)
		}
		return templates[i].Activation > templates[j].Activation
	})

	if len(templates) > 24 {
		templates = templates[:24]
	}

	return templates
}

func (field *composerLogicField) templateMatch(template logicTemplate, chord data.Chord, pos int) float64 {
	if chord.ActiveCount() == 0 || pos < template.Start || pos >= template.Start+len(template.Sequence) {
		return 0
	}

	expected := template.Sequence[pos-template.Start]
	return maxVariantOverlap(chord, logicVariants(expected, template.Program))
}

func (field *composerLogicField) prefixScore(prefix []data.Chord) float64 {
	if field == nil || len(prefix) == 0 {
		return 0
	}

	pos := len(prefix) - 1
	score := 0.0
	for _, template := range field.templates {
		match := field.templateMatch(template, prefix[pos], pos)
		if match == 0 {
			continue
		}

		score += template.Activation * match * 0.25
		if pos > 0 {
			prevMatch := field.templateMatch(template, prefix[pos-1], pos-1)
			if prevMatch > 0 {
				score += template.Activation * prevMatch * match * 0.4
			}
		}
		if pos == template.Start && template.Start == 0 && field.leftAnchor.ActiveCount() > 0 {
			score += template.Activation * logicOverlap(field.leftAnchor, template.Entry, template.Program) * match * 0.2
		}
	}

	return score
}

func (field *composerLogicField) spanScore(span []data.Chord) float64 {
	if field == nil || len(span) == 0 {
		return 0
	}

	total := 0.0
	for _, template := range field.templates {
		if template.Start < 0 || template.Start+len(template.Sequence) > len(span) {
			continue
		}

		coverage := 0.0
		for pos := template.Start; pos < template.Start+len(template.Sequence); pos++ {
			coverage += field.templateMatch(template, span[pos], pos)
		}
		coverage /= float64(len(template.Sequence))
		if coverage == 0 {
			continue
		}

		boundaryFit := 1.0
		if template.Start == 0 && field.leftAnchor.ActiveCount() > 0 {
			boundaryFit += logicOverlap(field.leftAnchor, template.Entry, template.Program) * 0.25
		}
		if template.Start+len(template.Sequence) == len(span) && field.rightAnchor.ActiveCount() > 0 {
			boundaryFit += logicOverlap(field.rightAnchor, template.Exit, template.Program) * 0.25
		}

		bonus := 0.0
		if coverage > 0.82 {
			bonus = template.Activation * 0.55 * float64(len(template.Sequence))
		}

		total += template.Activation * coverage * boundaryFit * (1.0 + float64(len(template.Sequence))*0.2)
		total += bonus
	}

	return total
}

func roleScale(role cortex.NodeRole) float64 {
	switch role {
	case cortex.RoleTool:
		return 1.0
	case cortex.RoleRegister:
		return 0.65
	default:
		return 0.8
	}
}

func logicOverlap(query, target, program data.Chord) float64 {
	if query.ActiveCount() == 0 || target.ActiveCount() == 0 {
		return 0
	}

	structural := resonance.OverlapScore(&query, &target)
	if program.ActiveCount() == 0 {
		return structural
	}

	control := resonance.OverlapScore(&query, &program)
	return structural*0.85 + control*0.15
}

func (field *composerLogicField) suggestions(pos, width int) map[data.Chord]float64 {
	if field == nil {
		return nil
	}

	suggestions := make(map[data.Chord]float64, len(field.rules)*3+len(field.chains)*3)
	depthFactor := 1.0
	if width > 1 {
		depthFactor = float64(pos+1) / float64(width)
	}

	for _, weighted := range field.rules {
		rule := weighted.Rule
		base := weighted.Activation * 0.08

		for idx, candidate := range logicVariants(rule.Payload, rule.Program) {
			suggestions[candidate] += base * variantScale(idx)
		}
		for idx, candidate := range logicVariants(rule.Interface, rule.Program) {
			suggestions[candidate] += base * 0.25 * variantScale(idx)
		}

		if pos == 0 && field.leftAnchor.ActiveCount() > 0 && rule.Payload.ActiveCount() > 0 {
			trigger := logicOverlap(field.leftAnchor, rule.Interface, rule.Program)
			if trigger > 0 {
				for idx, candidate := range logicVariants(rule.Payload, rule.Program) {
					suggestions[candidate] += weighted.Activation * (0.45 + trigger) * variantScale(idx)
				}
			}
		}

		if pos == width-1 && field.rightAnchor.ActiveCount() > 0 && rule.Interface.ActiveCount() > 0 {
			trigger := logicOverlap(field.rightAnchor, rule.Payload, rule.Program)
			if trigger > 0 {
				for idx, candidate := range logicVariants(rule.Interface, rule.Program) {
					suggestions[candidate] += weighted.Activation * (0.45 + trigger) * variantScale(idx)
				}
			}
		}

		for _, hint := range field.extraHints {
			if hint.ActiveCount() == 0 || rule.Payload.ActiveCount() == 0 {
				continue
			}
			trigger := logicOverlap(hint, rule.Interface, rule.Program)
			if trigger > 0 {
				for idx, candidate := range logicVariants(rule.Payload, rule.Program) {
					suggestions[candidate] += weighted.Activation * trigger * 0.4 * variantScale(idx)
				}
			}
		}
	}

	for _, weighted := range field.chains {
		chain := weighted.Chain
		bridgeChord := chooseBridgeChord(chain)
		leftTrigger := logicOverlap(field.leftAnchor, chain.Left.Interface, chain.Program)
		rightTrigger := logicOverlap(field.rightAnchor, chain.Right.Payload, chain.Program)

		for idx, candidate := range logicVariants(chain.Left.Payload, chain.Program) {
			if pos == 0 || width == 1 {
				suggestions[candidate] += weighted.Activation * (0.2 + leftTrigger) * variantScale(idx)
			}
		}

		for idx, candidate := range logicVariants(chain.Right.Payload, chain.Program) {
			if pos == width-1 || width == 1 {
				suggestions[candidate] += weighted.Activation * (0.2 + rightTrigger) * variantScale(idx)
			}
		}

		if bridgeChord.ActiveCount() > 0 {
			bridgeScore := 0.0
			if width == 1 {
				bridgeScore = weighted.Activation * (0.12 + leftTrigger*rightTrigger*1.4)
			} else if pos > 0 && pos < width-1 {
				bridgeScore = weighted.Activation * (0.12 + (leftTrigger+rightTrigger+depthFactor)*0.15)
			}

			if bridgeScore > 0 {
				for idx, candidate := range logicVariants(bridgeChord, chain.Program) {
					suggestions[candidate] += bridgeScore * variantScale(idx)
				}
			}
		}
	}

	for _, template := range field.templates {
		if pos < template.Start || pos >= template.Start+len(template.Sequence) {
			continue
		}

		expected := template.Sequence[pos-template.Start]
		base := template.Activation * 0.8
		if pos == template.Start && template.Start == 0 && field.leftAnchor.ActiveCount() > 0 {
			base += template.Activation * logicOverlap(field.leftAnchor, template.Entry, template.Program)
		}
		if pos == template.Start+len(template.Sequence)-1 && template.Start+len(template.Sequence) == width && field.rightAnchor.ActiveCount() > 0 {
			base += template.Activation * logicOverlap(field.rightAnchor, template.Exit, template.Program)
		}

		for idx, candidate := range logicVariants(expected, template.Program) {
			suggestions[candidate] += base * variantScale(idx)
		}
	}

	return suggestions
}

func logicVariants(chord, program data.Chord) []data.Chord {
	if chord.ActiveCount() == 0 {
		return nil
	}

	variants := make([]data.Chord, 0, 3)
	variants = append(variants, chord)

	if program.ActiveCount() == 0 {
		return variants
	}

	rot := geometry.RotationForChord(program)
	rotated := rot.ApplyToChord(chord)
	if rotated.ActiveCount() > 0 && rotated != chord {
		variants = append(variants, rotated)
	}

	reversed := rot.ReverseChord(chord)
	if reversed.ActiveCount() > 0 && reversed != chord && (len(variants) < 2 || variants[1] != reversed) {
		variants = append(variants, reversed)
	}

	return variants
}

func variantScale(idx int) float64 {
	if idx == 0 {
		return 1.0
	}
	return 0.35
}

func chooseBridgeChord(chain cortex.LogicChain) data.Chord {
	bridge := chain.Bridge
	if bridge.ActiveCount() >= 2 {
		return bridge
	}

	bridge = data.ChordOR(&chain.Left.Payload, &chain.Right.Interface)
	if chain.Program.ActiveCount() == 0 {
		return bridge
	}

	rot := geometry.RotationForChord(chain.Program)
	transformed := rot.ApplyToChord(bridge)
	if transformed.ActiveCount() > 0 {
		return transformed
	}

	return bridge
}

func (field *composerLogicField) unaryScore(pos, width int, chord data.Chord) float64 {
	if field == nil || chord.ActiveCount() == 0 {
		return 0
	}

	score := 0.0
	for _, weighted := range field.rules {
		rule := weighted.Rule

		for idx, candidate := range logicVariants(rule.Payload, rule.Program) {
			score += weighted.Activation * resonance.OverlapScore(&chord, &candidate) * 0.04 * variantScale(idx)
		}

		if pos == 0 && field.leftAnchor.ActiveCount() > 0 && rule.Payload.ActiveCount() > 0 {
			trigger := logicOverlap(field.leftAnchor, rule.Interface, rule.Program)
			if trigger > 0 {
				score += weighted.Activation * trigger * maxVariantOverlap(chord, logicVariants(rule.Payload, rule.Program)) * 1.5
			}
		}

		if pos == width-1 && field.rightAnchor.ActiveCount() > 0 && rule.Interface.ActiveCount() > 0 {
			trigger := logicOverlap(field.rightAnchor, rule.Payload, rule.Program)
			if trigger > 0 {
				score += weighted.Activation * trigger * maxVariantOverlap(chord, logicVariants(rule.Interface, rule.Program)) * 1.35
			}
		}

		for _, hint := range field.extraHints {
			if hint.ActiveCount() == 0 || rule.Payload.ActiveCount() == 0 {
				continue
			}
			trigger := logicOverlap(hint, rule.Interface, rule.Program)
			if trigger > 0 {
				score += weighted.Activation * trigger * maxVariantOverlap(chord, logicVariants(rule.Payload, rule.Program)) * 0.35
			}
		}
	}

	for _, weighted := range field.chains {
		chain := weighted.Chain
		bridge := chooseBridgeChord(chain)
		leftTrigger := logicOverlap(field.leftAnchor, chain.Left.Interface, chain.Program)
		rightTrigger := logicOverlap(field.rightAnchor, chain.Right.Payload, chain.Program)

		if width == 1 && leftTrigger > 0 && rightTrigger > 0 {
			score += weighted.Activation * leftTrigger * rightTrigger * maxVariantOverlap(chord, logicVariants(bridge, chain.Program)) * 1.9
		}

		if pos == 0 {
			score += weighted.Activation * leftTrigger * maxVariantOverlap(chord, logicVariants(chain.Left.Payload, chain.Program)) * 1.25
		}

		if pos == width-1 {
			score += weighted.Activation * rightTrigger * maxVariantOverlap(chord, logicVariants(chain.Right.Payload, chain.Program)) * 1.25
		}

		if pos > 0 && pos < width-1 {
			score += weighted.Activation * (leftTrigger + rightTrigger) * 0.2 * maxVariantOverlap(chord, logicVariants(bridge, chain.Program))
		}
	}

	for _, template := range field.templates {
		match := field.templateMatch(template, chord, pos)
		if match == 0 {
			continue
		}

		templateScore := template.Activation * match * 1.45
		if pos == template.Start && template.Start == 0 && field.leftAnchor.ActiveCount() > 0 {
			templateScore += template.Activation * logicOverlap(field.leftAnchor, template.Entry, template.Program) * match * 0.85
		}
		if pos == template.Start+len(template.Sequence)-1 && template.Start+len(template.Sequence) == width && field.rightAnchor.ActiveCount() > 0 {
			templateScore += template.Activation * logicOverlap(field.rightAnchor, template.Exit, template.Program) * match * 0.75
		}
		score += templateScore
	}

	return score
}

func maxVariantOverlap(chord data.Chord, variants []data.Chord) float64 {
	best := 0.0
	for _, candidate := range variants {
		overlap := resonance.OverlapScore(&chord, &candidate)
		if overlap > best {
			best = overlap
		}
	}
	return best
}

func (field *composerLogicField) transitionScore(left, right data.Chord) float64 {
	if field == nil || left.ActiveCount() == 0 || right.ActiveCount() == 0 {
		return 0
	}

	score := 0.0
	for _, weighted := range field.rules {
		rule := weighted.Rule
		if rule.Interface.ActiveCount() == 0 || rule.Payload.ActiveCount() == 0 {
			continue
		}

		trigger := logicOverlap(left, rule.Interface, rule.Program)
		if trigger == 0 {
			continue
		}

		emit := maxVariantOverlap(right, logicVariants(rule.Payload, rule.Program))
		if emit == 0 {
			continue
		}

		score += weighted.Activation * trigger * emit * 1.7
	}

	for _, weighted := range field.chains {
		chain := weighted.Chain
		firstLeg := logicOverlap(left, chain.Left.Interface, chain.Program)
		if firstLeg > 0 {
			score += weighted.Activation * firstLeg * maxVariantOverlap(right, logicVariants(chain.Left.Payload, chain.Program)) * 1.15
		}

		bridgeLeg := maxVariantOverlap(left, logicVariants(chooseBridgeChord(chain), chain.Program))
		if bridgeLeg > 0 {
			score += weighted.Activation * bridgeLeg * maxVariantOverlap(right, logicVariants(chain.Right.Payload, chain.Program)) * 0.95
		}
	}

	for _, template := range field.templates {
		for idx := 0; idx+1 < len(template.Sequence); idx++ {
			leftMatch := maxVariantOverlap(left, logicVariants(template.Sequence[idx], template.Program))
			if leftMatch == 0 {
				continue
			}

			rightMatch := maxVariantOverlap(right, logicVariants(template.Sequence[idx+1], template.Program))
			if rightMatch == 0 {
				continue
			}

			score += template.Activation * leftMatch * rightMatch * 1.35
		}
	}

	return score
}

func (field *composerLogicField) tripleScore(left, mid, right data.Chord) float64 {
	if field == nil || left.ActiveCount() == 0 || mid.ActiveCount() == 0 || right.ActiveCount() == 0 {
		return 0
	}

	score := 0.0
	for _, weighted := range field.chains {
		chain := weighted.Chain
		trigger := logicOverlap(left, chain.Left.Interface, chain.Program)
		if trigger == 0 {
			continue
		}

		middle := max(
			maxVariantOverlap(mid, logicVariants(chain.Left.Payload, chain.Program))*0.55+
				maxVariantOverlap(mid, logicVariants(chain.Right.Interface, chain.Program))*0.45,
			maxVariantOverlap(mid, logicVariants(chooseBridgeChord(chain), chain.Program)),
		)
		if middle == 0 {
			continue
		}

		emit := maxVariantOverlap(right, logicVariants(chain.Right.Payload, chain.Program))
		if emit == 0 {
			continue
		}

		score += weighted.Activation * trigger * middle * emit * 2.4
	}

	for _, template := range field.templates {
		for idx := 0; idx+2 < len(template.Sequence); idx++ {
			leftMatch := maxVariantOverlap(left, logicVariants(template.Sequence[idx], template.Program))
			if leftMatch == 0 {
				continue
			}

			midMatch := maxVariantOverlap(mid, logicVariants(template.Sequence[idx+1], template.Program))
			if midMatch == 0 {
				continue
			}

			rightMatch := maxVariantOverlap(right, logicVariants(template.Sequence[idx+2], template.Program))
			if rightMatch == 0 {
				continue
			}

			score += template.Activation * leftMatch * midMatch * rightMatch * 1.9
		}
	}

	return score
}

func (field *composerLogicField) repairSuggestions(span []data.Chord) []map[data.Chord]float64 {
	if field == nil || len(span) == 0 {
		return nil
	}

	repair := make([]map[data.Chord]float64, len(span))
	for i := range repair {
		repair[i] = make(map[data.Chord]float64)
	}

	emit := func(pos int, chord data.Chord, score float64) {
		if pos < 0 || pos >= len(repair) || chord.ActiveCount() == 0 || score <= 0 {
			return
		}
		repair[pos][chord] += score
	}

	prev := field.leftAnchor
	for pos, current := range span {
		for _, weighted := range field.rules {
			rule := weighted.Rule
			if prev.ActiveCount() > 0 && rule.Payload.ActiveCount() > 0 {
				trigger := logicOverlap(prev, rule.Interface, rule.Program)
				if trigger > 0.55 {
					match := maxVariantOverlap(current, logicVariants(rule.Payload, rule.Program))
					if match < 0.65 {
						for _, candidate := range logicVariants(rule.Payload, rule.Program) {
							emit(pos, candidate, weighted.Activation*trigger*(0.8-match))
						}
					}
				}
			}
		}
		prev = current
	}

	next := field.rightAnchor
	for pos := len(span) - 1; pos >= 0; pos-- {
		current := span[pos]
		for _, weighted := range field.rules {
			rule := weighted.Rule
			if next.ActiveCount() > 0 && rule.Interface.ActiveCount() > 0 {
				trigger := logicOverlap(next, rule.Payload, rule.Program)
				if trigger > 0.55 {
					match := maxVariantOverlap(current, logicVariants(rule.Interface, rule.Program))
					if match < 0.65 {
						for _, candidate := range logicVariants(rule.Interface, rule.Program) {
							emit(pos, candidate, weighted.Activation*trigger*(0.8-match))
						}
					}
				}
			}
		}
		next = current
	}

	left := field.leftAnchor
	for pos := 0; pos+1 < len(span); pos++ {
		mid := span[pos]
		right := span[pos+1]

		for _, weighted := range field.chains {
			chain := weighted.Chain
			trigger := logicOverlap(left, chain.Left.Interface, chain.Program)
			middle := maxVariantOverlap(mid, logicVariants(chooseBridgeChord(chain), chain.Program))
			emitScore := maxVariantOverlap(right, logicVariants(chain.Right.Payload, chain.Program))

			if trigger > 0.55 && middle < 0.6 {
				for _, candidate := range logicVariants(chooseBridgeChord(chain), chain.Program) {
					emit(pos, candidate, weighted.Activation*trigger*(0.8-middle))
				}
			}

			if trigger > 0.55 && emitScore < 0.6 {
				for _, candidate := range logicVariants(chain.Right.Payload, chain.Program) {
					emit(pos+1, candidate, weighted.Activation*trigger*(0.8-emitScore))
				}
			}
		}

		left = mid
	}

	for _, template := range field.templates {
		if template.Start < 0 || template.Start+len(template.Sequence) > len(span) {
			continue
		}

		coverage := 0.0
		for pos := template.Start; pos < template.Start+len(template.Sequence); pos++ {
			coverage += field.templateMatch(template, span[pos], pos)
		}
		coverage /= float64(len(template.Sequence))
		if coverage < 0.45 {
			continue
		}

		for pos := template.Start; pos < template.Start+len(template.Sequence); pos++ {
			match := field.templateMatch(template, span[pos], pos)
			if match >= 0.7 {
				continue
			}

			expected := template.Sequence[pos-template.Start]
			for _, candidate := range logicVariants(expected, template.Program) {
				emit(pos, candidate, template.Activation*(0.9-match)*(0.5+coverage))
			}
		}
	}

	return repair
}

func lastChord(chords []data.Chord) data.Chord {
	if len(chords) == 0 {
		return data.Chord{}
	}
	return chords[len(chords)-1]
}

func firstChord(chords []data.Chord) data.Chord {
	if len(chords) == 0 {
		return data.Chord{}
	}
	return chords[0]
}

func (composer *BoundaryComposer) buildPairPriors(boundary SpanBoundary, candidates []spanCandidate) []map[pairPriorKey]float64 {
	if boundary.Width <= 1 {
		return nil
	}

	priors := make([]map[pairPriorKey]float64, boundary.Width-1)
	for i := range priors {
		priors[i] = make(map[pairPriorKey]float64)
	}

	for _, candidate := range candidates {
		for pos := 0; pos+1 < len(candidate.Chords) && pos < len(priors); pos++ {
			left := candidate.Chords[pos]
			right := candidate.Chords[pos+1]
			if left.ActiveCount() == 0 || right.ActiveCount() == 0 {
				continue
			}
			priors[pos][pairPriorKey{Left: left, Right: right}] += candidate.Score
		}
	}

	return priors
}

func (composer *BoundaryComposer) buildTripletPriors(boundary SpanBoundary, candidates []spanCandidate) []map[triplePriorKey]float64 {
	if boundary.Width <= 2 {
		return nil
	}

	priors := make([]map[triplePriorKey]float64, boundary.Width-2)
	for i := range priors {
		priors[i] = make(map[triplePriorKey]float64)
	}

	for _, candidate := range candidates {
		for pos := 0; pos+2 < len(candidate.Chords) && pos < len(priors); pos++ {
			left := candidate.Chords[pos]
			mid := candidate.Chords[pos+1]
			right := candidate.Chords[pos+2]
			if left.ActiveCount() == 0 || mid.ActiveCount() == 0 || right.ActiveCount() == 0 {
				continue
			}
			priors[pos][triplePriorKey{Left: left, Mid: mid, Right: right}] += candidate.Score
		}
	}

	return priors
}

func (composer *BoundaryComposer) buildDomains(
	boundary SpanBoundary,
	priors []map[data.Chord]float64,
	logicField *composerLogicField,
) [][]data.Chord {
	domains := make([][]data.Chord, boundary.Width)

	for pos := 0; pos < boundary.Width; pos++ {
		scores := make(map[data.Chord]float64, len(priors[pos])+composer.domainLimit)
		for chord, score := range priors[pos] {
			scores[chord] += score
		}

		if logicField != nil {
			for chord, score := range logicField.suggestions(pos, boundary.Width) {
				scores[chord] += score
			}
		}

		if composer.field != nil {
			if pos == 0 && len(boundary.Left) > 0 {
				followers := composer.field.TopFollowers(boundary.Left[len(boundary.Left)-1], composer.domainLimit)
				for _, follower := range followers {
					scores[follower.Chord] += follower.Score * 0.8
				}
			}

			if pos == boundary.Width-1 && len(boundary.Right) > 0 {
				predecessors := composer.field.TopPredecessors(boundary.Right[0], composer.domainLimit)
				for _, predecessor := range predecessors {
					scores[predecessor.Chord] += predecessor.Score * 0.8
				}
			}

			if boundary.Width == 1 && len(boundary.Left) > 0 && len(boundary.Right) > 0 {
				middles := composer.field.TopMiddles(boundary.Left[len(boundary.Left)-1], boundary.Right[0], composer.domainLimit)
				for _, middle := range middles {
					scores[middle.Chord] += middle.Score * 1.2
				}
			}

			if len(boundary.Left) > 0 && len(boundary.Right) > 0 {
				bridges := composer.field.TopBridges(boundary.Left[len(boundary.Left)-1], boundary.Right[0], composer.domainLimit)
				scale := 0.6
				if boundary.Width > 1 {
					scale = 0.25 + 0.35*(1.0-math.Abs(float64(pos-(boundary.Width-1)/2))/float64(boundary.Width))
				}
				for _, bridge := range bridges {
					scores[bridge.Chord] += bridge.Score * scale
				}
			}

			for _, top := range composer.field.TopChords(composer.domainLimit) {
				scores[top.Chord] += top.Score * 0.05
			}
		}

		priors[pos] = composer.trimPrior(scores, max(composer.topK, composer.domainLimit*2))
		domains[pos] = rankChordMap(priors[pos], composer.domainLimit)
	}

	return domains
}

func rankChordMap(scores map[data.Chord]float64, limit int) []data.Chord {
	if len(scores) == 0 || limit <= 0 {
		return nil
	}

	type scored struct {
		Chord data.Chord
		Score float64
	}

	ranked := make([]scored, 0, len(scores))
	for chord, score := range scores {
		if chord.ActiveCount() == 0 || score <= 0 {
			continue
		}
		ranked = append(ranked, scored{Chord: chord, Score: score})
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return chordRank(ranked[i].Chord) < chordRank(ranked[j].Chord)
		}
		return ranked[i].Score > ranked[j].Score
	})

	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	result := make([]data.Chord, 0, len(ranked))
	for _, entry := range ranked {
		result = append(result, entry.Chord)
	}
	return result
}

func (composer *BoundaryComposer) mergeRepairSuggestions(
	domains [][]data.Chord,
	priors []map[data.Chord]float64,
	repair []map[data.Chord]float64,
) bool {
	if len(repair) == 0 {
		return false
	}

	changed := false
	for pos := range repair {
		if pos >= len(priors) {
			break
		}

		scores := make(map[data.Chord]float64, len(priors[pos])+len(domains[pos])+len(repair[pos]))
		for chord, score := range priors[pos] {
			scores[chord] = score
		}
		for _, chord := range domains[pos] {
			if _, ok := scores[chord]; !ok {
				scores[chord] = 0.001
			}
		}

		for chord, score := range repair[pos] {
			if score <= 0 || chord.ActiveCount() == 0 {
				continue
			}
			before := scores[chord]
			scores[chord] += score
			if scores[chord] > before {
				changed = true
			}
		}

		priors[pos] = composer.trimPrior(scores, max(composer.topK, composer.domainLimit*2))
		domains[pos] = rankChordMap(priors[pos], composer.domainLimit)
	}

	return changed
}

func (composer *BoundaryComposer) solveField(
	boundary SpanBoundary,
	domains [][]data.Chord,
	priors []map[data.Chord]float64,
	pairPriors []map[pairPriorKey]float64,
	tripletPriors []map[triplePriorKey]float64,
	logicField *composerLogicField,
) fieldSolution {
	if boundary.Width <= 0 || len(domains) != boundary.Width {
		return fieldSolution{}
	}

	for _, domain := range domains {
		if len(domain) == 0 {
			return fieldSolution{}
		}
	}

	if boundary.Width == 1 {
		return composer.solveSingle(boundary, domains, priors, pairPriors, tripletPriors, logicField)
	}

	if boundary.Width == 2 {
		return composer.solvePair(boundary, domains, priors, pairPriors, tripletPriors, logicField)
	}

	if logicField != nil && logicField.hasCircuits() {
		return composer.solveCircuitBeam(boundary, domains, priors, pairPriors, tripletPriors, logicField)
	}

	return composer.solveSecondOrder(boundary, domains, priors, pairPriors, tripletPriors, logicField)
}

func (composer *BoundaryComposer) solveSingle(
	boundary SpanBoundary,
	domains [][]data.Chord,
	priors []map[data.Chord]float64,
	pairPriors []map[pairPriorKey]float64,
	tripletPriors []map[triplePriorKey]float64,
	logicField *composerLogicField,
) fieldSolution {
	bestScore := math.Inf(-1)
	bestChord := data.Chord{}

	for _, chord := range domains[0] {
		score := composer.unaryPotential(boundary, 0, chord, priors, logicField)
		if len(boundary.Left) > 0 {
			score += composer.transitionPotential(-1, boundary.Left[len(boundary.Left)-1], chord, pairPriors, logicField)
		}
		if len(boundary.Right) > 0 {
			score += composer.transitionPotential(boundary.Width-1, chord, boundary.Right[0], pairPriors, logicField)
		}
		if len(boundary.Left) > 0 && len(boundary.Right) > 0 {
			score += composer.triplePotential(-1, boundary.Left[len(boundary.Left)-1], chord, boundary.Right[0], tripletPriors, logicField)
		}

		if score > bestScore || (score == bestScore && chordRank(chord) < chordRank(bestChord)) {
			bestScore = score
			bestChord = chord
		}
	}

	if bestChord.ActiveCount() == 0 || math.IsInf(bestScore, -1) {
		return fieldSolution{}
	}

	return fieldSolution{Span: []data.Chord{bestChord}, Score: bestScore}
}

func (composer *BoundaryComposer) solvePair(
	boundary SpanBoundary,
	domains [][]data.Chord,
	priors []map[data.Chord]float64,
	pairPriors []map[pairPriorKey]float64,
	tripletPriors []map[triplePriorKey]float64,
	logicField *composerLogicField,
) fieldSolution {
	bestScore := math.Inf(-1)
	bestLeft := -1
	bestRight := -1

	for leftIdx, left := range domains[0] {
		for rightIdx, right := range domains[1] {
			score := composer.unaryPotential(boundary, 0, left, priors, logicField)
			score += composer.unaryPotential(boundary, 1, right, priors, logicField)
			score += composer.transitionPotential(0, left, right, pairPriors, logicField)

			if len(boundary.Left) > 0 {
				score += composer.transitionPotential(-1, boundary.Left[len(boundary.Left)-1], left, pairPriors, logicField)
				score += composer.triplePotential(-1, boundary.Left[len(boundary.Left)-1], left, right, tripletPriors, logicField)
			}

			if len(boundary.Right) > 0 {
				score += composer.transitionPotential(boundary.Width-1, right, boundary.Right[0], pairPriors, logicField)
				score += composer.triplePotential(-1, left, right, boundary.Right[0], tripletPriors, logicField)
			}

			if score > bestScore || (score == bestScore && (bestLeft < 0 || chordRank(left) < chordRank(domains[0][bestLeft]))) {
				bestScore = score
				bestLeft = leftIdx
				bestRight = rightIdx
			}
		}
	}

	if bestLeft < 0 || bestRight < 0 || math.IsInf(bestScore, -1) {
		return fieldSolution{}
	}

	return fieldSolution{
		Span:  []data.Chord{domains[0][bestLeft], domains[1][bestRight]},
		Score: bestScore,
	}
}

func (composer *BoundaryComposer) solveSecondOrder(
	boundary SpanBoundary,
	domains [][]data.Chord,
	priors []map[data.Chord]float64,
	pairPriors []map[pairPriorKey]float64,
	tripletPriors []map[triplePriorKey]float64,
	logicField *composerLogicField,
) fieldSolution {
	width := boundary.Width
	dpPrev := make([][]float64, len(domains[0]))
	for idx := range dpPrev {
		dpPrev[idx] = make([]float64, len(domains[1]))
		for j := range dpPrev[idx] {
			dpPrev[idx][j] = math.Inf(-1)
		}
	}

	back := make([][][]int, width)
	for pos := range back {
		if pos < 2 {
			continue
		}
		back[pos] = make([][]int, len(domains[pos-1]))
		for prevIdx := range back[pos] {
			back[pos][prevIdx] = make([]int, len(domains[pos]))
			for currIdx := range back[pos][prevIdx] {
				back[pos][prevIdx][currIdx] = -1
			}
		}
	}

	for idx0, chord0 := range domains[0] {
		for idx1, chord1 := range domains[1] {
			score := composer.unaryPotential(boundary, 0, chord0, priors, logicField)
			score += composer.unaryPotential(boundary, 1, chord1, priors, logicField)
			score += composer.transitionPotential(0, chord0, chord1, pairPriors, logicField)

			if len(boundary.Left) > 0 {
				score += composer.transitionPotential(-1, boundary.Left[len(boundary.Left)-1], chord0, pairPriors, logicField)
				score += composer.triplePotential(-1, boundary.Left[len(boundary.Left)-1], chord0, chord1, tripletPriors, logicField)
			}

			dpPrev[idx0][idx1] = score
		}
	}

	for pos := 2; pos < width; pos++ {
		dpCurr := make([][]float64, len(domains[pos-1]))
		for prevIdx := range dpCurr {
			dpCurr[prevIdx] = make([]float64, len(domains[pos]))
			for currIdx := range dpCurr[prevIdx] {
				dpCurr[prevIdx][currIdx] = math.Inf(-1)
			}
		}

		for prevPrevIdx, prevRow := range dpPrev {
			prevPrevChord := domains[pos-2][prevPrevIdx]

			for prevIdx, baseScore := range prevRow {
				if math.IsInf(baseScore, -1) {
					continue
				}

				prevChord := domains[pos-1][prevIdx]

				for currIdx, currChord := range domains[pos] {
					candidate := baseScore
					candidate += composer.unaryPotential(boundary, pos, currChord, priors, logicField)
					candidate += composer.transitionPotential(pos-1, prevChord, currChord, pairPriors, logicField)
					candidate += composer.triplePotential(pos-2, prevPrevChord, prevChord, currChord, tripletPriors, logicField)

					if candidate > dpCurr[prevIdx][currIdx] || (candidate == dpCurr[prevIdx][currIdx] && (back[pos][prevIdx][currIdx] < 0 || chordRank(prevPrevChord) < chordRank(domains[pos-2][back[pos][prevIdx][currIdx]]))) {
						dpCurr[prevIdx][currIdx] = candidate
						back[pos][prevIdx][currIdx] = prevPrevIdx
					}
				}
			}
		}

		dpPrev = dpCurr
	}

	lastPos := width - 1
	bestScore := math.Inf(-1)
	bestPrev := -1
	bestCurr := -1

	for prevIdx, row := range dpPrev {
		for currIdx, score := range row {
			if math.IsInf(score, -1) {
				continue
			}

			candidate := score
			lastChord := domains[lastPos][currIdx]
			prevChord := domains[lastPos-1][prevIdx]

			if len(boundary.Right) > 0 {
				candidate += composer.transitionPotential(lastPos, lastChord, boundary.Right[0], pairPriors, logicField)
				candidate += composer.triplePotential(-1, prevChord, lastChord, boundary.Right[0], tripletPriors, logicField)
			}

			if candidate > bestScore || (candidate == bestScore && (bestCurr < 0 || chordRank(lastChord) < chordRank(domains[lastPos][bestCurr]))) {
				bestScore = candidate
				bestPrev = prevIdx
				bestCurr = currIdx
			}
		}
	}

	if bestPrev < 0 || bestCurr < 0 || math.IsInf(bestScore, -1) {
		return fieldSolution{}
	}

	span := make([]data.Chord, width)
	span[lastPos] = domains[lastPos][bestCurr]
	span[lastPos-1] = domains[lastPos-1][bestPrev]

	currIdx := bestCurr
	prevIdx := bestPrev
	for pos := lastPos; pos >= 2; pos-- {
		prevPrevIdx := back[pos][prevIdx][currIdx]
		if prevPrevIdx < 0 {
			return fieldSolution{}
		}

		span[pos-2] = domains[pos-2][prevPrevIdx]
		currIdx = prevIdx
		prevIdx = prevPrevIdx
	}

	return fieldSolution{Span: span, Score: bestScore}
}

func (composer *BoundaryComposer) unaryPotential(
	boundary SpanBoundary,
	pos int,
	chord data.Chord,
	priors []map[data.Chord]float64,
	logicField *composerLogicField,
) float64 {
	score := priors[pos][chord]
	score += composer.hintAffinity(chord, boundary.Hints)

	if logicField != nil {
		score += logicField.unaryScore(pos, boundary.Width, chord)
	}

	if composer.field != nil && len(boundary.Left) > 0 && len(boundary.Right) > 0 {
		scale := 0.2
		if boundary.Width == 1 {
			scale = 1.0
		}
		score += composer.field.BridgeScore(boundary.Left[len(boundary.Left)-1], chord, boundary.Right[0]) * scale
	}

	return score
}

func (composer *BoundaryComposer) transitionPotential(
	pos int,
	left data.Chord,
	right data.Chord,
	pairPriors []map[pairPriorKey]float64,
	logicField *composerLogicField,
) float64 {
	score := 0.0
	if composer.field != nil {
		score += composer.field.PairScore(left, right) * 2.0
	}

	if pos >= 0 && pos < len(pairPriors) {
		score += pairPriors[pos][pairPriorKey{Left: left, Right: right}] * 1.5
	}

	if logicField != nil {
		score += logicField.transitionScore(left, right)
	}

	return score
}

func (composer *BoundaryComposer) triplePotential(
	pos int,
	left data.Chord,
	mid data.Chord,
	right data.Chord,
	tripletPriors []map[triplePriorKey]float64,
	logicField *composerLogicField,
) float64 {
	score := 0.0
	if composer.field != nil {
		score += composer.field.TripletScore(left, mid, right) * 2.4
	}

	if pos >= 0 && pos < len(tripletPriors) {
		score += tripletPriors[pos][triplePriorKey{Left: left, Mid: mid, Right: right}] * 1.8
	}

	if logicField != nil {
		score += logicField.tripleScore(left, mid, right)
	}

	return score
}

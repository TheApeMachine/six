package programmer

import (
	"math/bits"
	"strconv"
	"strings"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

type Firmware struct{}

/*
Scheduler is the narrowest pool surface Firmware.Chain needs to re-enter
itself on behalf of a Value. pool.Queue.Submit matches this signature
exactly so wiring in production is a direct hand-off; tests inject a
synchronous stub that runs the task inline. Defining the interface in
programmer keeps the rule-walking helper usable from any caller
(gossip.Conn, vm.Orchestrator, …) without dragging pool or gossip into
each other's import graphs.
*/
type Scheduler interface {
	Submit(task func() *Executable)
}

/*
NewFirmware creates a new Firmware.
*/
func NewFirmware() *Firmware {
	return &Firmware{}
}

/*
Next evaluates the conditions of a Value and potentially assign a new firmware.
*/
func (firmware *Firmware) Next(value *primitive.Value) string {
	for _, rule := range core.Cfg.Value.Rules {
		if firmware.evaluateConditions(value, rule.Conditions, true) {
			return rule.Firmware
		}
	}

	return ""
}

/*
Chain resolves the next Executable for a Value by consulting the rule
evaluator, attaching a Finalizer that re-submits the Value through the
same Scheduler so it walks the chain one ALU pass at a time. When no
rule fires, the Value has reached steady state and its resident program
word is what the substrate should execute. The optional terminal
Finalizer is attached to that resident Executable so callers get a
single "chain complete" hook — this is how the orchestrator stages a
finished Value's Signals+Context+Gradient+Properties into the next
Value's Asset region for gossip-style propagation. When no terminal
is supplied, the resident Executable carries no Finalizer and the
chain stops cleanly after the resident pass.

Chain is the single place that decides "what does the ALU run for this
Value right now". Every production entry point (gossip.Conn.Write and
vm.Orchestrator.Cycle today) submits through Chain so the rule engine
fires uniformly regardless of where a Value enters the pipeline.
*/
func (firmware *Firmware) Chain(
	scheduler Scheduler,
	value *primitive.Value,
	terminal ...Finalizer,
) *Executable {
	if firmware == nil || scheduler == nil || value == nil {
		return nil
	}

	firmwareName := firmware.Next(value)

	if firmwareName == "" {
		// Steady state — no bootstrap rule fires. The Value's own
		// program word drives the ALU from here. If the caller wants
		// a "chain complete" hook (Orchestrator uses this to stage
		// results into the next Value's Asset), attach it to the
		// resident Executable so Backend.Dispatch fires it after the
		// resident pass lands.
		executable := NewResidentExecutable(value)

		if len(terminal) > 0 && terminal[0] != nil {
			executable.SetFinalizer(terminal[0])
		}

		return executable
	}

	executable := NewExecutable(value, firmwareName, nil)

	// Chain the next pass. Once the substrate finishes this firmware
	// and the Finalizer runs, re-enter the evaluator so the Value
	// progresses to the next rule (or to its resident program) on
	// the very next pool dispatch. The terminal hook is threaded
	// through every hop so it survives the full link → affinity →
	// resident walk.
	executable.SetFinalizer(func(finalized *primitive.Value) {
		scheduler.Submit(func() *Executable {
			return firmware.Chain(scheduler, finalized, terminal...)
		})
	})

	return executable
}

func (firmware *Firmware) evaluateConditions(value *primitive.Value, conditions map[string]any, isAnd bool) bool {
	if len(conditions) == 0 {
		return true
	}

	for key, val := range conditions {
		var match bool

		lowerKey := strings.ToLower(strings.TrimSpace(key))

		switch lowerKey {
		case "and":
			if sub, ok := val.(map[string]any); ok {
				match = firmware.evaluateConditions(value, sub, true)
			} else {
				match = false
			}
		case "or":
			if sub, ok := val.(map[string]any); ok {
				match = firmware.evaluateConditions(value, sub, false)
			} else {
				match = false
			}
		default:
			// Keys may be a bare region name ("signals") or a region
			// sub-span written like the five-column DSL operand ref
			// ("signals[0,1]", "asset[8,8]"). Sub-spans let rules gate
			// on a single word or a narrow slab without introducing a
			// second condition vocabulary — the same bracket syntax
			// programs already use also describes the state those
			// programs read from and write to.
			regionName, regionStart, regionSpan, refOK := parseRegionRef(lowerKey)

			if !refOK {
				match = false
			} else if regionType, nameOK := primitive.RegionNames[regionName]; !nameOK {
				match = false
			} else {
				region := value.Get(regionType)
				slice := sliceRegion(region, regionStart, regionSpan)

				switch v := val.(type) {
				case bool:
					hasBits := firmware.HasBits(slice)
					match = (v && hasBits) || (!v && !hasBits)
				default:
					match = false
				}
			}
		}

		if isAnd && !match {
			return false
		}
		if !isAnd && match {
			return true
		}
	}

	return isAnd
}

func (firmware *Firmware) HasBits(region []uint64) bool {
	for _, word := range region {
		if bits.OnesCount64(word) > 0 {
			return true
		}
	}

	return false
}

/*
parseRegionRef splits a condition key into its region name and optional
[start,span] sub-range. A bare name leaves start and span at zero, which
sliceRegion reads as "the whole region." Malformed brackets return
refOK=false so the evaluator can reject the condition without panicking
on a typo in config.yml.
*/
func parseRegionRef(key string) (name string, start int, span int, refOK bool) {
	open := strings.IndexByte(key, '[')

	if open < 0 {
		return key, 0, 0, true
	}

	if !strings.HasSuffix(key, "]") {
		return "", 0, 0, false
	}

	name = strings.TrimSpace(key[:open])
	body := key[open+1 : len(key)-1]

	parts := strings.Split(body, ",")

	if len(parts) != 2 {
		return "", 0, 0, false
	}

	parsedStart, err := strconv.Atoi(strings.TrimSpace(parts[0]))

	if err != nil || parsedStart < 0 {
		return "", 0, 0, false
	}

	parsedSpan, err := strconv.Atoi(strings.TrimSpace(parts[1]))

	if err != nil || parsedSpan <= 0 {
		return "", 0, 0, false
	}

	return name, parsedStart, parsedSpan, true
}

/*
sliceRegion clamps a [start,span] sub-range against the backing region and
returns the requested slab. A zero span (bare region name) returns the
whole region so bare rules keep their historical "any bit set anywhere"
semantics. Out-of-range requests collapse to an empty slice, which
HasBits treats as "no bits set" — the safe interpretation for a
misconfigured gate.
*/
func sliceRegion(region []uint64, start, span int) []uint64 {
	if span == 0 {
		return region
	}

	if start >= len(region) {
		return nil
	}

	end := start + span

	if end > len(region) {
		end = len(region)
	}

	return region[start:end]
}

package programmer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/theapemachine/six/pkg/core"
)

/*
RegionRef is a parsed region[start,span] slice — the DSL's only way to
point at Value state. Name indexes into core.Cfg.Value.Region, Start is a
word offset inside that region, Span is a word count. Span >= 1 always;
a bare region[index] parses as Span=1.
*/
type RegionRef struct {
	Name     string
	Start    int
	Span     int
	absStart int
}

/*
regionLayout is the absolute word range a named region occupies inside a
Value. StartWord is 0..127, WordCount is the region's size in 64-bit words.
Lookup is routed through core.Cfg so configuration can still move regions.
*/
type regionLayout struct {
	StartWord int
	WordCount int
}

/*
ParseRegionRef turns a single DSL operand string into a validated RegionRef.
It resolves the region layout against core.Cfg so an out-of-range start or
span is caught at parse time rather than at staging time.
*/
func ParseRegionRef(label string) (RegionRef, error) {
	label = strings.TrimSpace(label)

	openIdx := strings.IndexByte(label, '[')
	if openIdx < 0 || !strings.HasSuffix(label, "]") {
		return RegionRef{}, fmt.Errorf("programmer: invalid region ref %q", label)
	}

	name := label[:openIdx]
	inner := label[openIdx+1 : len(label)-1]

	commaIdx := strings.IndexByte(inner, ',')
	var startStr, spanStr string
	if commaIdx >= 0 {
		startStr = strings.TrimSpace(inner[:commaIdx])
		spanStr = strings.TrimSpace(inner[commaIdx+1:])
	} else {
		startStr = strings.TrimSpace(inner)
	}

	start, err := strconv.Atoi(startStr)
	if err != nil {
		return RegionRef{}, fmt.Errorf("programmer: invalid region ref %q: %w", label, err)
	}

	span := 1
	if spanStr != "" {
		span, err = strconv.Atoi(spanStr)
		if err != nil {
			return RegionRef{}, fmt.Errorf("programmer: invalid region ref %q: %w", label, err)
		}
	}

	if span < 1 {
		return RegionRef{}, fmt.Errorf("programmer: region ref %q span must be >= 1", label)
	}

	ref := RegionRef{Name: name, Start: start, Span: span}

	layout, err := regionLayoutByName(ref.Name)
	if err != nil {
		return RegionRef{}, err
	}

	if start < 0 || start+span > layout.WordCount {
		return RegionRef{}, fmt.Errorf(
			"programmer: region ref %q out of range (region %q holds %d words)",
			label, ref.Name, layout.WordCount,
		)
	}

	ref.absStart = layout.StartWord + ref.Start

	return ref, nil
}

/*
AbsStart is the absolute word index of the slice's first word in a Value.
Used by staging/writeback paths that need a raw uint64 offset.
*/
func (ref RegionRef) AbsStart() int {
	return ref.absStart
}

/*
regionLayoutByName resolves a DSL region name against the live core.Cfg
layout. The "reserved" band (words 56..117) is named implicitly because it
does not have its own offset struct: it is whatever is left between Properties
and the kernel-transport/identity words, which today means 62 words.
*/
func regionLayoutByName(name string) (regionLayout, error) {
	if core.Cfg == nil {
		return regionLayout{}, fmt.Errorf("programmer: core.Cfg not initialized")
	}

	region := core.Cfg.Value.Region

	switch name {
	case "tokens":
		return layoutFrom(region.Tokens), nil
	case "program":
		return layoutFrom(region.Program), nil
	case "signals":
		return layoutFrom(region.Signals), nil
	case "context":
		return layoutFrom(region.Context), nil
	case "gradient":
		return layoutFrom(region.Gradient), nil
	case "properties":
		return layoutFrom(region.Properties), nil
	case "prev":
		return layoutFrom(region.Prev), nil
	case "next":
		return layoutFrom(region.Next), nil
	case "id":
		return layoutFrom(region.ID), nil
	case "affinity":
		return layoutFrom(region.Affinity), nil
	case "reserved":
		return regionLayout{StartWord: 56, WordCount: 62}, nil
	}

	return regionLayout{}, fmt.Errorf("programmer: unknown region %q", name)
}

/*
layoutFrom converts a ValueOffsetConfig (start word + bit width) into the
word-count form staging/writeback need. Fractional-bit regions like the
257-bit affinity round up to the containing word count.
*/
func layoutFrom(offset core.ValueOffsetConfig) regionLayout {
	return regionLayout{
		StartWord: offset.Start,
		WordCount: int((offset.Bits + 63) / 64),
	}
}

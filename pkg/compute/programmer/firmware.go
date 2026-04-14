package programmer

import (
	"math/bits"
	"strings"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

type Firmware struct{}

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

func (firmware *Firmware) evaluateConditions(value *primitive.Value, conditions map[string]any, isAnd bool) bool {
	if len(conditions) == 0 {
		return true
	}

	for key, val := range conditions {
		var match bool

		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if lowerKey == "and" {
			if sub, ok := val.(map[string]any); ok {
				match = firmware.evaluateConditions(value, sub, true)
			} else {
				match = false
			}
		} else if lowerKey == "or" {
			if sub, ok := val.(map[string]any); ok {
				match = firmware.evaluateConditions(value, sub, false)
			} else {
				match = false
			}
		} else {
			regionType, nameOK := primitive.RegionNames[lowerKey]
			if !nameOK {
				match = false
			} else {
				region := value.Get(regionType)
				switch v := val.(type) {
				case bool:
					hasBits := firmware.HasBits(region)
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

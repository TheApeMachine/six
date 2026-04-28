package primitive

import (
	"fmt"

	"github.com/theapemachine/six/pkg/core"
)

type EmitOptions func(*Value)

/*
Emit is a convenience function that acts as a generic builder with
enough granular control so it can be used anywhere we need to emit
one or more Values.
*/
func Emit(options ...EmitOptions) *Value {
	value := AllocValue()
	value = value.StampID()

	for _, option := range options {
		option(value)
	}

	return value
}

func WithLabels(labels ...uint64) EmitOptions {
	return func(value *Value) {
		for _, label := range labels {
			if label == 0 {
				continue
			}

			value.SetProperty(LABELS, label)
		}
	}
}

func WithTTL(ttl uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(TTL), ttl)
	}
}

func WithNoise(noise uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(NOISE), noise)
	}
}

func WithCommunity(community uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(COMMUNITY), community)
	}
}

func WithStatus(status uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(STATUS), status)
	}
}

func WithFieldID(fieldID uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(TARGET), fieldID)
	}
}

func WithConfidence(confidence uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(CONFIDENCE), confidence)
	}
}

func WithEpoch(epoch uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(EPOCH), epoch)
	}
}

func WithProbeState(probeState uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(STATUS), probeState)
	}
}

func WithProgramID(programID uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(PROGRAM_ID), programID)
	}
}

func WithTemperature(temperature uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(TEMPERATURE), temperature)
	}
}

func WithRole(role uint64) EmitOptions {
	return func(value *Value) {
		if role == 0 {
			return
		}

		value.Set(core.Cfg.Value.Region.Properties.Start+int(ROLE), role)
	}
}

func WithTarget(target uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(TARGET), target)
	}
}

// WithProgram copies pre-compiled program words into the configured program region.
func WithProgram(words []uint64) EmitOptions {
	return func(value *Value) {
		if value == nil {
			return
		}

		value.InstallProgram(words)
	}
}

// WithFirmware lowers named config firmware into the program region (same bits Dispatch executes).
func WithFirmware(firmware core.FirmwareType) EmitOptions {
	return func(value *Value) {
		if value.InstallFirmware(firmware) {
			return
		}

		panic(fmt.Errorf("primitive: firmware %q is missing or empty", firmware))
	}
}

func WithReference(reference uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Properties.Start+int(REFERENCE), reference)
	}
}

/*
WithContext writes one word into the Value's context region at the
given offset. Firmware that uses `B.{{A.context[N,1]}}` /
`{{A.context[N,1]}}` operands reads these words at install time to
resolve the actual operand offset / threshold value, so the option
must appear BEFORE WithFirmware in the option chain.

Offset is the slot inside the context region (0 to contextWords-1),
not the absolute frame word.
*/
func WithContext(offset int, word uint64) EmitOptions {
	return func(value *Value) {
		value.Set(core.Cfg.Value.Region.Context.Start+offset, word)
	}
}

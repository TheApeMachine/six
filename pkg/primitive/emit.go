package primitive

import (
	"encoding/binary"
	"io"

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
	value.StampNewID()

	for _, option := range options {
		option(value)
	}

	return value
}

func WithLabels(labels ...uint64) EmitOptions {
	return func(value *Value) {
		base := core.Cfg.Value.Region.Properties.Start + int(LABELS)
		for i, label := range labels {
			value.Set(base+i, label)
		}
	}
}

func WithTTL(ttl uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(TTL), ttl)
	}
}

func WithNoise(noise uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(NOISE), noise)
	}
}

func WithCommunity(community uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(COMMUNITY), community)
	}
}

func WithStatus(status uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(STATUS), status)
	}
}

func WithFieldID(fieldID uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(TARGET), fieldID)
	}
}

func WithConfidence(confidence uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(CONFIDENCE), confidence)
	}
}

func WithEpoch(epoch uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(EPOCH), epoch)
	}
}

func WithProbeState(probeState uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(STATUS), probeState)
	}
}

func WithProbeWindow(probeWindow uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(WINDOW), probeWindow)
	}
}

func WithProbeDepth(probeDepth uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(DEPTH), probeDepth)
	}
}

func WithRole(role uint64) EmitOptions {
	return func(value *Value) {
		if role == 0 {
			return
		}

		value.Set(int(ROLE), role)
	}
}

func WithTarget(target uint64) EmitOptions {
	return func(value *Value) {
		value.Set(int(TARGET), target)
	}
}

// WithProgram copies pre-compiled program words into the configured program region.
func WithProgram(words []uint64) EmitOptions {
	return func(value *Value) {
		if value == nil {
			return
		}

		value.WriteProgramWords(words)
	}
}

// WithFirmware lowers named config firmware into the program region (same bits Dispatch executes).
func WithFirmware(firmware core.FirmwareType) EmitOptions {
	return func(value *Value) {
		if value == nil {
			return
		}

		entry := core.Cfg.Programs[firmware]
		value.WriteProgramWords(entry.Compiled())
		value.SetSchedulingNext(entry.ResolveSchedulingNext(value.ID()))
	}
}

// WithAssetPressureMetrics materializes Coverage / Consensus / Crystallization into the asset
// window and sets Properties TTL=1 via a full-frame patch so the stamped ID from Emit is preserved.
func WithAssetPressureMetrics(coverage, consensus, crystallization float64) EmitOptions {
	return func(value *Value) {
		if value == nil {
			return
		}

		clamp01 := func(x float64) float64 {
			if x < 0 {
				return 0
			}

			if x > 1 {
				return 1
			}

			return x
		}

		scaleFixed := func(x float64) uint64 {
			return uint64(clamp01(x) * float64(uint64(1)<<32))
		}

		buf := make([]byte, core.Cfg.Value.Bytes)

		if _, err := value.Read(buf); err != nil && err != io.EOF {
			return
		}

		assetStart, assetWords := core.Cfg.Value.Region.Asset.WordExtent()

		if assetWords >= 3 {
			binary.LittleEndian.PutUint64(buf[(assetStart+0)*8:], scaleFixed(coverage))
			binary.LittleEndian.PutUint64(buf[(assetStart+1)*8:], scaleFixed(consensus))
			binary.LittleEndian.PutUint64(buf[(assetStart+2)*8:], scaleFixed(crystallization))
		}

		binary.LittleEndian.PutUint64(buf[(core.Cfg.Value.Region.Properties.Start+int(TTL))*8:], 1)

		_ = value.LoadFullFrame(buf)
	}
}

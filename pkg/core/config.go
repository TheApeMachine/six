package core

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

/*
SubstrateSleepIdle is the fixed ticker interval for sleep consolidation.
The Backend always runs this loop when a sleep sample is wired; the
interval is not YAML-configurable so consolidation cannot vanish behind a
zero duration.
*/
const SubstrateSleepIdle = 5 * time.Second

/*
DefaultTokenSettleMaxPasses replaces non-positive tokenSettleMaxPasses so
post-UB settling always runs instead of treating zero as “off”.
*/
const DefaultTokenSettleMaxPasses = 4

var (
	Cfg *Config
)

func init() {
	Cfg = NewConfig()
}

type FirmwareType string

const (
	LINK               FirmwareType = "link"
	AFFINITY           FirmwareType = "affinity"
	POPCOUNT           FirmwareType = "popcount"
	COUPLING           FirmwareType = "coupling"
	BEAM               FirmwareType = "beam"
	SWARM              FirmwareType = "swarm"
	STEP               FirmwareType = "step"
	EMERGENCE          FirmwareType = "emergence"
	EXPLORE            FirmwareType = "explore"
	HUB                FirmwareType = "hub"
	UNSUPERVISED_LEARN FirmwareType = "unsupervised_learn"
	MEASURE_FIELD      FirmwareType = "measure_field"
	CLASSIFY_READOUT   FirmwareType = "classify_readout"
	EPISODIC_REPLAY    FirmwareType = "episodic_replay"
	INTERVENTION       FirmwareType = "intervene"
)

type SystemConfig struct {
	BatchSize          int           `mapstructure:"batchSize"`
	BatchWindow        time.Duration `mapstructure:"batchWindow"`
	QueueSize          int           `mapstructure:"queueSize"`
	ShannonLimit       float64       `mapstructure:"shannonLimit"`
	ResonanceThreshold float64       `mapstructure:"resonanceThreshold"`
	BeliefEpsilon      float64       `mapstructure:"beliefEpsilon"`
}

type ProgramConfig struct {
	Name     string   `mapstructure:"name"`
	Source   string   `mapstructure:"source"`
	Compiled []uint64 `mapstructure:"compiled"`
}

/*
ValueConfig holds the configuration for a Value.
*/
type ValueConfig struct {
	Word         int                `mapstructure:"word"`
	Words        int                `mapstructure:"words"`
	Bytes        int                `mapstructure:"bytes"`
	NumRotations int                `mapstructure:"num_rotations"`
	Region       ValueRegionConfig  `mapstructure:"region"`
	Opcodes      ValueOpcodesConfig `mapstructure:"opcodes"`
}

/*
ValueRegionConfig holds the configuration for a Value's region.

Layout (128 uint64 words = 1 KiB):

	Tokens:   words  0–15  (1024 bits; 128 B Morton slab, up to 64 × 16-bit codes)
	Program:  words  16–23  (512 bits)
	Signals:  words  24–31  (512 bits)
	Context:    words  32–39  (512 bits)
	Gradient:   words  40–47  (512 bits)
	Properties: words  48–63  (1024 bits; canonical property / forward-transition band)
	Asset:      words 64–119  (3584 bits; scratch + bundled program payload; words 118–119 are kernel frame metadata — see kernel/frame_meta.go)
	Prev:     word  120
	Next:     word  121
	ID:       word  122
	Affinity: words 123–127 (257 bits, Fermat prime width)
*/
type ValueRegionConfig struct {
	Tokens     ValueOffsetConfig `mapstructure:"tokens"`
	Program    ValueOffsetConfig `mapstructure:"program"`
	Signals    ValueOffsetConfig `mapstructure:"signals"`
	Context    ValueOffsetConfig `mapstructure:"context"`
	Gradient   ValueOffsetConfig `mapstructure:"gradient"`
	Properties ValueOffsetConfig `mapstructure:"properties"`
	Asset      ValueOffsetConfig `mapstructure:"asset"`
	Prev       ValueOffsetConfig `mapstructure:"prev"`
	Next       ValueOffsetConfig `mapstructure:"next"`
	ID         ValueOffsetConfig `mapstructure:"id"`
	Affinity   ValueOffsetConfig `mapstructure:"affinity"`
}

/*
MaxTokenIngestBytes is the largest raw byte span passed to primitive.NewValue
from fixed-width ingest (e.g. vm.Tokenizer). With Morton encoding each input
byte occupies one uint64 word, so the max is simply the number of token words.
*/
func (region ValueRegionConfig) MaxTokenIngestBytes() int {
	out := int(region.Tokens.Bits / 64)

	if out < 1 {
		return 1
	}

	return out
}

/*
ValueOffsetConfig holds the configuration
for a Value's offset.
*/
type ValueOffsetConfig struct {
	Start int    `mapstructure:"start"`
	Bits  uint64 `mapstructure:"bits"`
}

/*
WordExtent returns the absolute uint64 word index where the region begins and
how many words cover Bits (rounded up). Region slices and ALU operand packing
both use this shape.
*/
func (cfg ValueOffsetConfig) WordExtent() (start int, words int) {
	return cfg.Start, int((cfg.Bits + 63) / 64)
}

/*
ValueOpcodesConfig holds the configuration
for a Value's opcodes.
*/
type ValueOpcodesConfig struct {
	False    string `mapstructure:"false"`
	And      string `mapstructure:"and"`
	AandNotB string `mapstructure:"aandnotb"`
	A        string `mapstructure:"a"`
	NotAandB string `mapstructure:"notandb"`
	B        string `mapstructure:"b"`
	XOR      string `mapstructure:"xor"`
	OR       string `mapstructure:"or"`
	NOR      string `mapstructure:"nor"`
	XNOR     string `mapstructure:"xnor"`
	NOTB     string `mapstructure:"notb"`
	IFBTHENA string `mapstructure:"ifbthena"`
	NOTA     string `mapstructure:"nota"`
	IFATHENB string `mapstructure:"ifathenb"`
	NAND     string `mapstructure:"nand"`
	TRUE     string `mapstructure:"true"`
}

/*
Config wraps viper with strict typed accessors that refuse to
return zero-values for missing keys.
*/
type Config struct {
	System   SystemConfig
	Programs map[FirmwareType]ProgramConfig
	Value    ValueConfig

	// TelemetryEnabled controls whether the WebSocket client for raw Value wire frames is started.
	TelemetryEnabled bool

	// TelemetryWebSocketURL is the bridge WebSocket URL (e.g. ws://127.0.0.1:3000/ws).
	TelemetryWebSocketURL string
}

func NewConfig() *Config {
	Cfg = &Config{
		System: SystemConfig{
			BatchSize:          WithDefault(viper.GetInt("system.batchSize"), 10000),
			BatchWindow:        time.Duration(WithDefault(viper.GetInt("system.batchWindow"), 500)) * time.Microsecond,
			QueueSize:          WithDefault(viper.GetInt("system.queueSize"), 20000),
			ShannonLimit:       WithDefault(viper.GetFloat64("system.shannonLimit"), 0.47),
			ResonanceThreshold: WithDefault(viper.GetFloat64("system.resonanceThreshold"), 0.6),
			BeliefEpsilon:      WithDefault(viper.GetFloat64("system.beliefEpsilon"), 0.05),
		},
		Programs: precompile(),
		Value: ValueConfig{
			Word:         WithDefault(viper.GetInt("value.word"), 64),
			Words:        WithDefault(viper.GetInt("value.words"), 128),
			Bytes:        WithDefault(viper.GetInt("value.bytes"), 1024),
			NumRotations: WithDefault(viper.GetInt("value.num_rotations"), 16),
			Region: ValueRegionConfig{
				Tokens: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.tokens.start"), 0),
					Bits:  WithDefault(viper.GetUint64("value.region.tokens.bits"), 1024),
				},
				Program: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.program.start"), 16),
					Bits:  WithDefault(viper.GetUint64("value.region.program.bits"), 512),
				},
				Signals: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.signals.start"), 24),
					Bits:  WithDefault(viper.GetUint64("value.region.signals.bits"), 512),
				},
				Context: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.context.start"), 32),
					Bits:  WithDefault(viper.GetUint64("value.region.context.bits"), 512),
				},
				Gradient: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.gradient.start"), 40),
					Bits:  WithDefault(viper.GetUint64("value.region.gradient.bits"), 512),
				},
				Properties: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.properties.start"), 48),
					Bits:  WithDefault(viper.GetUint64("value.region.properties.bits"), 1024),
				},
				Asset: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.asset.start"), 64),
					Bits:  WithDefault(viper.GetUint64("value.region.asset.bits"), 3584),
				},
				Prev: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.prev.start"), 120),
					Bits:  WithDefault(viper.GetUint64("value.region.prev.bits"), 64),
				},
				Next: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.next.start"), 121),
					Bits:  WithDefault(viper.GetUint64("value.region.next.bits"), 64),
				},
				ID: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.id.start"), 122),
					Bits:  WithDefault(viper.GetUint64("value.region.id.bits"), 64),
				},
				Affinity: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.affinity.start"), 123),
					Bits:  WithDefault(viper.GetUint64("value.region.affinity.bits"), 257),
				},
			},
			Opcodes: ValueOpcodesConfig{
				False:    WithDefault(viper.GetString("value.opcodes.false"), "0000"),
				And:      WithDefault(viper.GetString("value.opcodes.and"), "0001"),
				AandNotB: WithDefault(viper.GetString("value.opcodes.aandnotb"), "0010"),
				A:        WithDefault(viper.GetString("value.opcodes.a"), "0011"),
				NotAandB: WithDefault(viper.GetString("value.opcodes.notandb"), "0100"),
				B:        WithDefault(viper.GetString("value.opcodes.b"), "0101"),
				XOR:      WithDefault(viper.GetString("value.opcodes.xor"), "0110"),
				OR:       WithDefault(viper.GetString("value.opcodes.or"), "0111"),
				NOR:      WithDefault(viper.GetString("value.opcodes.nor"), "1000"),
				XNOR:     WithDefault(viper.GetString("value.opcodes.xnor"), "1001"),
				NOTB:     WithDefault(viper.GetString("value.opcodes.notb"), "1010"),
				IFBTHENA: WithDefault(viper.GetString("value.opcodes.ifbthena"), "1011"),
				NOTA:     WithDefault(viper.GetString("value.opcodes.nota"), "1100"),
				IFATHENB: WithDefault(viper.GetString("value.opcodes.ifathenb"), "1101"),
				NAND:     WithDefault(viper.GetString("value.opcodes.nand"), "1110"),
				TRUE:     WithDefault(viper.GetString("value.opcodes.true"), "1111"),
			},
		},
		TelemetryEnabled:      WithDefault(viper.GetBool("telemetry.enabled"), false),
		TelemetryWebSocketURL: WithDefault(viper.GetString("telemetry.ws_url"), ""),
	}

	return Cfg
}

func WithDefault[T comparable](value, defaultValue T) T {
	var zero T

	if value == zero {
		return defaultValue
	}

	return value
}

func precompile() map[FirmwareType]ProgramConfig {
	out := make(map[FirmwareType]ProgramConfig)

	raw, ok := viper.Get("programs").(map[string]any)
	if !ok || raw == nil {
		return out
	}

	for key, val := range raw {
		source, ok := val.(string)
		if !ok || source == "" {
			continue
		}

		ft := FirmwareType(key)
		name := viper.GetString(fmt.Sprintf("programs.%s.name", key))
		if name == "" {
			name = key
		}

		out[ft] = ProgramConfig{
			Name:     name,
			Source:   source,
			Compiled: compile(source),
		}
	}

	return out
}

// compile packs firmware source text into up to eight uint64 words (the default
// program region size). primitive.WriteProgramWords consumes this slice; it is
// not a semantic DSL lowering.
func compile(source string) []uint64 {
	if len(source) == 0 {
		return nil
	}

	const maxWords = 8

	src := []byte(source)
	nWords := (len(src) + 7) / 8
	if nWords > maxWords {
		nWords = maxWords
	}

	out := make([]uint64, 0, nWords)

	for w := 0; w < nWords; w++ {
		var word uint64

		for b := 0; b < 8; b++ {
			idx := w*8 + b
			if idx >= len(src) {
				break
			}

			word |= uint64(src[idx]) << (8 * b)
		}

		out = append(out, word)
	}

	return out
}

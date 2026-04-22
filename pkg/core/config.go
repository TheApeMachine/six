package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/theapemachine/six/pkg/compute/program"
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
	FOLD_SUBSTRATE     FirmwareType = "fold_substrate"
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
	HYPOTHESIS         FirmwareType = "hypothesis"
	FALSIFICATION      FirmwareType = "falsification"
	CAUSAL_EXPLORE     FirmwareType = "causal_explore"
	CAUSAL_HUB         FirmwareType = "causal_hub"
)

type SystemConfig struct {
	BatchSize          int           `mapstructure:"batchSize"`
	BatchWindow        time.Duration `mapstructure:"batchWindow"`
	QueueSize          int           `mapstructure:"queueSize"`
	ShannonLimit       float64       `mapstructure:"shannonLimit"`
	ResonanceThreshold float64       `mapstructure:"resonanceThreshold"`
	BeliefEpsilon      float64       `mapstructure:"beliefEpsilon"`
	RouteBudget        int           `mapstructure:"routeBudget"`
	/*
		MaxMembersPerField is a hard backstop on how many Values a single
		leaf Field may absorb. The XOR-saturation gate (ShannonLimit) is
		mathematically a "fingerprint randomness" check: it trips for
		uncorrelated populations whose folded affinity approaches 50% set
		bits, but stays silent forever for highly-correlated populations
		whose XOR contributions cancel out. Real workloads (e.g. many
		fragments of the same prompt) are exactly that correlated case,
		which lets a single Field grow unboundedly even when intuition
		says the cluster is long past saturated.

		MaxMembersPerField caps that growth at a value the routing layer
		can defend regardless of fingerprint statistics: once a field is
		full, findCommunity stops considering it as a routing target,
		forcing either a sibling (within Hamming budget) or a fresh
		spawn. Zero disables the backstop.
	*/
	MaxMembersPerField int `mapstructure:"maxMembersPerField"`
	// QuiescenceTimeout bounds the busy-wait in vm.Orchestrator.Cycle before
	// draining; zero means 100ms at runtime.
	QuiescenceTimeout time.Duration `mapstructure:"quiescenceTimeout"`
	// DrainTimeout caps the post-quiescence drain loop; zero means 100ms at runtime.
	DrainTimeout time.Duration `mapstructure:"drainTimeout"`
}

/*
ProgramConfig caches the lowering of a named DSL block: the original Source
text (for diagnostics and tooling), the packed instruction Words ready to
load into a Value's program region, and SchedulingNext — the value the
install path must write into kernel.SchedulingNextProgramWord (word 117).

When SelfNext is true, SchedulingNext is a sentinel: callers replace it with
the resident Value's ID at install time so `next self` resolves dynamically.
A literal `next <id>` line leaves SelfNext false and SchedulingNext set to
the parsed ID.
*/
type ProgramConfig struct {
	Name           string
	Source         string
	Words          []uint64
	SchedulingNext uint64
	SelfNext       bool
}

/*
Compiled returns the program's instruction words. Kept as a method so call
sites that previously read .Compiled keep working with minimal noise.
*/
func (p ProgramConfig) Compiled() []uint64 { return p.Words }

/*
ResolveSchedulingNext picks the actual scheduler word for an install: when
the program declared `next self` it returns the resident Value's ID;
otherwise the literal continuation (0 = no follow-up).
*/
func (p ProgramConfig) ResolveSchedulingNext(residentValueID uint64) uint64 {
	if p.SelfNext {
		return residentValueID
	}

	return p.SchedulingNext
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

	Tokens:     words  0–15   (1024 bits; 128 B Morton slab, up to 64 × 16-bit codes)
	Program:    words  16–31  (1024 bits; up to 16 packed 64-bit instruction words)
	Signals:    words  32–39  (512 bits)
	Context:    words  40–47  (512 bits)
	Gradient:   words  48–55  (512 bits)
	Properties: words  56–71  (1024 bits; canonical property / forward-transition band)
	Asset:      words 72–119  (3072 bits; scratch + bundled program payload; words 117 = scheduler hop, 118–119 = kernel frame metadata)
	Prev:       word  120
	Next:       word  121
	ID:         word  122
	Affinity:   words 123–127 (257 bits, Fermat prime width)
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
	value := ValueConfig{
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
				Bits:  WithDefault(viper.GetUint64("value.region.program.bits"), 1024),
			},
			Signals: ValueOffsetConfig{
				Start: WithDefault(viper.GetInt("value.region.signals.start"), 32),
				Bits:  WithDefault(viper.GetUint64("value.region.signals.bits"), 512),
			},
			Context: ValueOffsetConfig{
				Start: WithDefault(viper.GetInt("value.region.context.start"), 40),
				Bits:  WithDefault(viper.GetUint64("value.region.context.bits"), 512),
			},
			Gradient: ValueOffsetConfig{
				Start: WithDefault(viper.GetInt("value.region.gradient.start"), 48),
				Bits:  WithDefault(viper.GetUint64("value.region.gradient.bits"), 512),
			},
			Properties: ValueOffsetConfig{
				Start: WithDefault(viper.GetInt("value.region.properties.start"), 56),
				Bits:  WithDefault(viper.GetUint64("value.region.properties.bits"), 1024),
			},
			Asset: ValueOffsetConfig{
				Start: WithDefault(viper.GetInt("value.region.asset.start"), 72),
				Bits:  WithDefault(viper.GetUint64("value.region.asset.bits"), 3072),
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
	}

	quiescenceTimeout := viper.GetDuration("system.quiescenceTimeout")
	if quiescenceTimeout == 0 {
		quiescenceTimeout = 100 * time.Millisecond
	}

	drainTimeout := viper.GetDuration("system.drainTimeout")
	if drainTimeout == 0 {
		drainTimeout = 100 * time.Millisecond
	}

	Cfg = &Config{
		System: SystemConfig{
			BatchSize:          WithDefault(viper.GetInt("system.batchSize"), 10000),
			BatchWindow:        time.Duration(WithDefault(viper.GetInt("system.batchWindow"), 500)) * time.Microsecond,
			QueueSize:          WithDefault(viper.GetInt("system.queueSize"), 20000),
			ShannonLimit:       WithDefault(viper.GetFloat64("system.shannonLimit"), 0.47),
			ResonanceThreshold: WithDefault(viper.GetFloat64("system.resonanceThreshold"), 0.6),
			BeliefEpsilon:      WithDefault(viper.GetFloat64("system.beliefEpsilon"), 0.05),
			RouteBudget:        WithDefault(viper.GetInt("system.routeBudget"), 128),
			MaxMembersPerField: WithDefault(viper.GetInt("system.maxMembersPerField"), 256),
			QuiescenceTimeout:  quiescenceTimeout,
			DrainTimeout:       drainTimeout,
		},
		Value:                 value,
		Programs:              precompile(value),
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

/*
precompile lowers every named DSL block under `programs:` into the packed
64-bit instruction format the universal-bitwise kernel executes directly.

This runs once during config load: nothing else in the runtime ever parses
DSL source. Lowering errors are surfaced as a panic because a malformed
firmware block is a programmer-authored bug we want caught at startup, not
silently elided into a no-op program.
*/
func precompile(value ValueConfig) map[FirmwareType]ProgramConfig {
	out := make(map[FirmwareType]ProgramConfig)

	raw, ok := viper.Get("programs").(map[string]any)
	if !ok || raw == nil {
		return out
	}

	layout := buildProgramLayout(value)
	_, maxWords := value.Region.Program.WordExtent()

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

		compiled, err := program.Compile(source, layout)
		if err != nil {
			panic(fmt.Errorf("config: program %q failed to compile: %w", key, err))
		}

		if maxWords > 0 && len(compiled.Words) > maxWords {
			panic(fmt.Errorf(
				"config: program %q lowered to %d instructions but program region only holds %d words",
				key, len(compiled.Words), maxWords,
			))
		}

		out[ft] = ProgramConfig{
			Name:           name,
			Source:         source,
			Words:          compiled.Words,
			SchedulingNext: compiled.SchedulingNext,
			SelfNext:       compiled.HasSelfNext,
		}
	}

	return out
}

// buildProgramLayout snapshots the active region and opcode tables into the
// compiler's neutral Layout type so program/ never has to import core.
func buildProgramLayout(value ValueConfig) program.Layout {
	r := value.Region

	regions := map[string]program.RegionExtent{
		"tokens":     extentFor(r.Tokens),
		"program":    extentFor(r.Program),
		"signals":    extentFor(r.Signals),
		"context":    extentFor(r.Context),
		"gradient":   extentFor(r.Gradient),
		"properties": extentFor(r.Properties),
		"asset":      extentFor(r.Asset),
		"prev":       extentFor(r.Prev),
		"next":       extentFor(r.Next),
		"id":         extentFor(r.ID),
		"affinity":   extentFor(r.Affinity),
	}

	opcodes := map[string]uint64{
		"false":    nibbleOf(value.Opcodes.False, 0x0),
		"and":      nibbleOf(value.Opcodes.And, 0x1),
		"aandnotb": nibbleOf(value.Opcodes.AandNotB, 0x2),
		"a":        nibbleOf(value.Opcodes.A, 0x3),
		"notandb":  nibbleOf(value.Opcodes.NotAandB, 0x4),
		"b":        nibbleOf(value.Opcodes.B, 0x5),
		"xor":      nibbleOf(value.Opcodes.XOR, 0x6),
		"or":       nibbleOf(value.Opcodes.OR, 0x7),
		"nor":      nibbleOf(value.Opcodes.NOR, 0x8),
		"xnor":     nibbleOf(value.Opcodes.XNOR, 0x9),
		"notb":     nibbleOf(value.Opcodes.NOTB, 0xA),
		"ifbthena": nibbleOf(value.Opcodes.IFBTHENA, 0xB),
		"nota":     nibbleOf(value.Opcodes.NOTA, 0xC),
		"ifathenb": nibbleOf(value.Opcodes.IFATHENB, 0xD),
		"nand":     nibbleOf(value.Opcodes.NAND, 0xE),
		"true":     nibbleOf(value.Opcodes.TRUE, 0xF),
	}

	return program.Layout{Regions: regions, Opcodes: opcodes}
}

func extentFor(cfg ValueOffsetConfig) program.RegionExtent {
	start, words := cfg.WordExtent()

	return program.RegionExtent{Start: start, Words: words}
}

// nibbleOf parses a 4-character binary string from the YAML opcode table
// (e.g. "0110" → 0x6) and falls back to the canonical default if the entry
// is malformed or missing.
func nibbleOf(spec string, fallback uint64) uint64 {
	spec = strings.TrimSpace(spec)
	if len(spec) == 0 {
		return fallback
	}

	value, err := strconv.ParseUint(spec, 2, 8)
	if err != nil || value > 0xF {
		return fallback
	}

	return value
}

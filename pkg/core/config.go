package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	_ = LoadDefaultConfig()
	Cfg = NewConfig()
}

/*
LoadDefaultConfig loads the repository default config when callers use the
library without going through cmd.initConfig. Explicit caller-loaded viper
state wins; this only fills the otherwise-empty firmware/config gap.
*/
func LoadDefaultConfig() error {
	if viper.IsSet("programs") {
		return nil
	}

	viper.SetConfigType("yml")

	var lastErr error
	for _, path := range defaultConfigPaths() {
		viper.SetConfigFile(path)
		err := viper.ReadInConfig()
		if err == nil {
			return nil
		}

		lastErr = err
	}

	return lastErr
}

func defaultConfigPaths() []string {
	paths := []string{
		filepath.Clean("cmd/cfg/config.yml"),
		filepath.Clean("config.yml"),
	}

	if _, file, _, ok := runtime.Caller(0); ok {
		paths = append(paths, filepath.Clean(
			filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"),
		))
	}

	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".six", "config.yml"))
	}

	seen := make(map[string]struct{}, len(paths))
	out := paths[:0]

	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}

		seen[path] = struct{}{}
		out = append(out, path)
	}

	return out
}

type FirmwareType string

const (
	LINK                 FirmwareType = "link"
	AFFINITY             FirmwareType = "affinity"
	FOLD_SUBSTRATE       FirmwareType = "structural_component"
	POPCOUNT             FirmwareType = "popcount"
	COUPLING             FirmwareType = "coupling"
	BEAM                 FirmwareType = "beam"
	SWARM                FirmwareType = "swarm"
	STEP                 FirmwareType = "step"
	EMERGENCE            FirmwareType = "emergence"
	EXPLORE              FirmwareType = "explore"
	HUB                  FirmwareType = "hub"
	UNSUPERVISED_LEARN   FirmwareType = "unsupervised_learn"
	VOTE_SWARM           FirmwareType = "vote_swarm"
	SURVEY_COMMUNITY     FirmwareType = "survey_community"
	MEASURE_FIELD        FirmwareType = "measure_field"
	CLASSIFY_READOUT     FirmwareType = "classify_readout"
	CLASS_PROTOTYPE      FirmwareType = "class_prototype"
	STRUCTURAL_ASSOCIATE FirmwareType = "structural_associate"
	STRUCTURAL_READOUT   FirmwareType = "structural_readout"
	EPISODIC_REPLAY      FirmwareType = "episodic_replay"
	INTERVENTION         FirmwareType = "intervene"
	HYPOTHESIS           FirmwareType = "hypothesis"
	FALSIFICATION        FirmwareType = "falsification"
	PROGRAM_SELECT       FirmwareType = "program_select"
	PROGRAM_CARRIER      FirmwareType = "program_carrier"
	CAUSAL_EXPLORE       FirmwareType = "causal_explore"
	CAUSAL_HUB           FirmwareType = "causal_hub"
	RECRUIT_COMMUNITY    FirmwareType = "recruit_community"
	QUERY                FirmwareType = "query"
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
	// QuiescenceTimeout bounds the busy-wait in vm.Machine.Cycle before
	// draining; zero means 100ms at runtime.
	QuiescenceTimeout time.Duration `mapstructure:"quiescenceTimeout"`
	// DrainTimeout caps the post-quiescence drain loop; zero means 100ms at runtime.
	DrainTimeout time.Duration `mapstructure:"drainTimeout"`
}

/*
ProgramConfig caches the lowering of a named DSL block: the original
Source text, the packed instruction Words ready to load into a Value's
program region, and the constants the compiler reserved in the asset
region. Constants and MaskTrueWord must be staged in the Value frame
before dispatch so the predicate primitive's threshold reads return the
expected literal.
*/
type ProgramConfig struct {
	Name          string
	Source        string
	Words         []uint64
	Constants     []program.ConstantInit
	Substitutions []program.Substitution
	MaskTrueWord  uint64
}

/*
Compiled returns the program's instruction words. Kept as a method so call
sites that previously read .Compiled keep working with minimal noise.
*/
func (p ProgramConfig) Compiled() []uint64 { return p.Words }

/*
ValueConfig holds the configuration for a Value.
*/
type ValueConfig struct {
	Word           int                `mapstructure:"word"`
	Words          int                `mapstructure:"words"`
	Bytes          int                `mapstructure:"bytes"`
	NumRotations   int                `mapstructure:"num_rotations"`
	Region         ValueRegionConfig  `mapstructure:"region"`
	Opcodes        ValueOpcodesConfig `mapstructure:"opcodes"`
	PropertiesList []string           `mapstructure:"properties"`
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
MaxTokenIngestBytes is the largest raw byte span that fits in one Value token
slab. Each uint64 token word carries four 16-bit Morton codes.
*/
func (region ValueRegionConfig) MaxTokenIngestBytes() int {
	out := int(region.Tokens.Bits/64) * 4

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
	system := SystemConfig{
		BatchSize:          configInt("system.batchSize", 10000),
		BatchWindow:        time.Duration(configInt("system.batchWindow", 500)) * time.Microsecond,
		QueueSize:          configInt("system.queueSize", 20000),
		ShannonLimit:       configFloat64("system.shannonLimit", 0.47),
		ResonanceThreshold: configFloat64("system.resonanceThreshold", 0.6),
		BeliefEpsilon:      configFloat64("system.beliefEpsilon", 0.05),
		RouteBudget:        configInt("system.routeBudget", 128),
		MaxMembersPerField: configInt("system.maxMembersPerField", 256),
		QuiescenceTimeout:  configDuration("system.quiescenceTimeout", 100*time.Millisecond),
		DrainTimeout:       configDuration("system.drainTimeout", 100*time.Millisecond),
	}

	value := ValueConfig{
		Word:         configInt("value.word", 64),
		Words:        configInt("value.words", 128),
		Bytes:        configInt("value.bytes", 1024),
		NumRotations: configInt("value.num_rotations", 16),
		Region: ValueRegionConfig{
			Tokens: ValueOffsetConfig{
				Start: configInt("value.region.tokens.start", 0),
				Bits:  configUint64("value.region.tokens.bits", 1024),
			},
			Program: ValueOffsetConfig{
				Start: configInt("value.region.program.start", 16),
				Bits:  configUint64("value.region.program.bits", 1024),
			},
			Signals: ValueOffsetConfig{
				Start: configInt("value.region.signals.start", 32),
				Bits:  configUint64("value.region.signals.bits", 512),
			},
			Context: ValueOffsetConfig{
				Start: configInt("value.region.context.start", 40),
				Bits:  configUint64("value.region.context.bits", 512),
			},
			Gradient: ValueOffsetConfig{
				Start: configInt("value.region.gradient.start", 48),
				Bits:  configUint64("value.region.gradient.bits", 512),
			},
			Properties: ValueOffsetConfig{
				Start: configInt("value.region.properties.start", 56),
				Bits:  configUint64("value.region.properties.bits", 1024),
			},
			Asset: ValueOffsetConfig{
				Start: configInt("value.region.asset.start", 72),
				Bits:  configUint64("value.region.asset.bits", 3072),
			},
			Prev: ValueOffsetConfig{
				Start: configInt("value.region.prev.start", 120),
				Bits:  configUint64("value.region.prev.bits", 64),
			},
			Next: ValueOffsetConfig{
				Start: configInt("value.region.next.start", 121),
				Bits:  configUint64("value.region.next.bits", 64),
			},
			ID: ValueOffsetConfig{
				Start: configInt("value.region.id.start", 122),
				Bits:  configUint64("value.region.id.bits", 64),
			},
			Affinity: ValueOffsetConfig{
				Start: configInt("value.region.affinity.start", 123),
				Bits:  configUint64("value.region.affinity.bits", 257),
			},
		},
		Opcodes: ValueOpcodesConfig{
			False:    configString("value.opcodes.false", "0000"),
			And:      configString("value.opcodes.and", "0001"),
			AandNotB: configString("value.opcodes.aandnotb", "0010"),
			A:        configString("value.opcodes.a", "0011"),
			NotAandB: configString("value.opcodes.notandb", "0100"),
			B:        configString("value.opcodes.b", "0101"),
			XOR:      configString("value.opcodes.xor", "0110"),
			OR:       configString("value.opcodes.or", "0111"),
			NOR:      configString("value.opcodes.nor", "1000"),
			XNOR:     configString("value.opcodes.xnor", "1001"),
			NOTB:     configString("value.opcodes.notb", "1010"),
			IFBTHENA: configString("value.opcodes.ifbthena", "1011"),
			NOTA:     configString("value.opcodes.nota", "1100"),
			IFATHENB: configString("value.opcodes.ifathenb", "1101"),
			NAND:     configString("value.opcodes.nand", "1110"),
			TRUE:     configString("value.opcodes.true", "1111"),
		},
		PropertiesList: viper.GetStringSlice("value.properties"),
	}

	Cfg = &Config{
		System:                system,
		Value:                 value,
		Programs:              precompile(value, system),
		TelemetryEnabled:      configBool("telemetry.enabled", false),
		TelemetryWebSocketURL: configString("telemetry.ws_url", ""),
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

func configInt(key string, defaultValue int) int {
	if !viper.IsSet(key) {
		return defaultValue
	}

	return viper.GetInt(key)
}

func configUint64(key string, defaultValue uint64) uint64 {
	if !viper.IsSet(key) {
		return defaultValue
	}

	return viper.GetUint64(key)
}

func configFloat64(key string, defaultValue float64) float64 {
	if !viper.IsSet(key) {
		return defaultValue
	}

	return viper.GetFloat64(key)
}

func configString(key string, defaultValue string) string {
	if !viper.IsSet(key) {
		return defaultValue
	}

	return viper.GetString(key)
}

func configBool(key string, defaultValue bool) bool {
	if !viper.IsSet(key) {
		return defaultValue
	}

	return viper.GetBool(key)
}

func configDuration(key string, defaultValue time.Duration) time.Duration {
	if !viper.IsSet(key) {
		return defaultValue
	}

	value := viper.GetDuration(key)
	if value == 0 {
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
func precompile(value ValueConfig, system SystemConfig) map[FirmwareType]ProgramConfig {
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

		source = expandProgramConstants(source, system)

		ft := FirmwareType(key)
		name := viper.GetString(fmt.Sprintf("programs.%s.name", key))
		if name == "" {
			name = key
		}

		compiled, err := program.Compile(context.Background(), source, layout)
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
			Name:          name,
			Source:        source,
			Words:         compiled.Words,
			Constants:     compiled.Constants,
			Substitutions: compiled.Substitutions,
			MaskTrueWord:  compiled.MaskTrueWord,
		}
	}

	return out
}

func expandProgramConstants(source string, system SystemConfig) string {
	replacer := strings.NewReplacer(
		"{{routeBudget}}", strconv.Itoa(system.RouteBudget),
	)

	return replacer.Replace(source)
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

	propertiesMap := make(map[string]int)
	for i, prop := range value.PropertiesList {
		propertiesMap[strings.ToLower(strings.TrimSpace(prop))] = i
	}

	statusValue := make(map[string]uint64)
	seenCanonical := make(map[string]struct{})

	for idx, status := range viper.GetStringSlice("value.status") {
		name := strings.TrimSpace(status)
		if name == "" {
			continue
		}

		canonical := strings.ToLower(name)
		if _, dup := seenCanonical[canonical]; dup {
			continue
		}

		seenCanonical[canonical] = struct{}{}
		idxU := uint64(idx)

		statusValue[name] = idxU
		statusValue[strings.ToUpper(canonical)] = idxU
		statusValue[canonical] = idxU
	}

	return program.Layout{
		Regions:     regions,
		Properties:  propertiesMap,
		Opcodes:     opcodes,
		StatusValue: statusValue,
	}
}

func extentFor(cfg ValueOffsetConfig) program.RegionExtent {
	start, words := cfg.WordExtent()

	return program.RegionExtent{Start: start, Words: words, Bits: cfg.Bits}
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

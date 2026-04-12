package core

import (
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

	// programLoader is installed by pkg/compute/programmer at init so
	// NewConfig can hand it the raw programs map without importing
	// programmer (which would cycle via compute → core).
	programLoader func(map[string]string) error
)

/*
RegisterProgramLoader wires a callback that consumes the raw programs
map and parses it into the programmer registry. It is invoked from
NewConfig after the map is loaded. Installing a nil loader clears the
hook so tests can re-register.
*/
func RegisterProgramLoader(loader func(map[string]string) error) {
	programLoader = loader

	if loader != nil && Cfg != nil && len(Cfg.Programs) > 0 {
		_ = loader(Cfg.Programs)
	}
}

func init() {
	Cfg = NewConfig()
}

type FirmwareType uint

const (
	FirmwareTypeLearn FirmwareType = iota
	FirmwareTypeBootloader
	FirmwareTypeTombstone
	FirmwareTypeViral
	FirmwareTypeBuild
	FirmwareTypeQuery
	FirmwareTypePrompt
)

type KadabraConfig struct {
	Bits                     int       `mapstructure:"bits"`
	BucketSize               int       `mapstructure:"bucketSize"`
	ReplicationFactor        int       `mapstructure:"replicationFactor"`
	MaxMeshPeers             int       `mapstructure:"maxMeshPeers"`
	Alpha                    int       `mapstructure:"alpha"`
	EpochQueries             int       `mapstructure:"epochQueries"`
	Penalty                  float64   `mapstructure:"penalty"`
	SecurityThreshold        float64   `mapstructure:"securityThreshold"`
	BucketSecurityThresholds []float64 `mapstructure:"bucketSecurityThresholds"`
	ShannonLimit             int       `mapstructure:"shannonLimit"`
	ClusterThreshold         int       `mapstructure:"clusterThreshold"`
}

type MarkovTrieConfig struct {
	DecayFactor                     float64 `mapstructure:"decayFactor"`
	EndToken                        string  `mapstructure:"endToken"`
	MaximumPathLength               int     `mapstructure:"maximumPathLength"`
	InterpolationSuffixDepth        int     `mapstructure:"interpolationSuffixDepth"`
	ClassificationContext           int     `mapstructure:"classificationContext"`
	CoOccurrenceWindow              int     `mapstructure:"coOccurrenceWindow"`
	PruneInterval                   int     `mapstructure:"pruneInterval"`
	PruneMinimumCount               float64 `mapstructure:"pruneMinimumCount"`
	ReplayLength                    int     `mapstructure:"replayLength"`
	ReplayThreshold                 float64 `mapstructure:"replayThreshold"`
	BeamWidth                       int     `mapstructure:"beamWidth"`
	UnknownProbability              float64 `mapstructure:"unknownProbability"`
	AdditiveSmoothing               float64 `mapstructure:"additiveSmoothing"`
	RecentPenalty                   float64 `mapstructure:"recentPenalty"`
	RecentWindow                    int     `mapstructure:"recentWindow"`
	EditDistance                    int     `mapstructure:"editDistance"`
	EditSimilarity                  float64 `mapstructure:"editSimilarity"`
	NgramConfidenceFloor            float64 `mapstructure:"ngramConfidenceFloor"`
	SymbolMinimumTotal              int     `mapstructure:"symbolMinimumTotal"`
	SymbolMinimumScore              float64 `mapstructure:"symbolMinimumScore"`
	SymbolLimit                     int     `mapstructure:"symbolLimit"`
	BaselineLearningRate            float64 `mapstructure:"baselineLearningRate"`
	MaxLearningRate                 float64 `mapstructure:"maxLearningRate"`
	SurprisalScaleBits              float64 `mapstructure:"surprisalScaleBits"`
	ConceptLabelPrefix              string  `mapstructure:"conceptLabelPrefix"`
	UnsupervisedConfidence          float64 `mapstructure:"unsupervisedConfidence"`
	ExperienceEmptyLabel            string  `mapstructure:"experienceEmptyLabel"`
	EpisodicCapacity                int     `mapstructure:"episodicCapacity"`
	EpisodicNeighborLimit           int     `mapstructure:"episodicNeighborLimit"`
	EpisodicRecencyWeight           float64 `mapstructure:"episodicRecencyWeight"`
	EpisodicBlendWeight             float64 `mapstructure:"episodicBlendWeight"`
	InitialConceptCounter           int     `mapstructure:"initialConceptCounter"`
	BPEEndOfWordToken               string  `mapstructure:"bpeEndOfWordToken"`
	BPEPairDelimiter                string  `mapstructure:"bpePairDelimiter"`
	PredictExperienceSurprisalBits  float64 `mapstructure:"predictExperienceSurprisalBits"`
	Temperature                     float64 `mapstructure:"temperature"`
	AdaptiveEMAAlpha                float64 `mapstructure:"adaptiveEMAAlpha"`
	AdaptiveMinSamples              int     `mapstructure:"adaptiveMinSamples"`
	AdaptiveMaxSamples              int     `mapstructure:"adaptiveMaxSamples"`
	AdaptiveMaxDepth                int     `mapstructure:"adaptiveMaxDepth"`
	AdaptiveMaxDepthDecay           float64 `mapstructure:"adaptiveMaxDepthDecay"`
	AdaptiveMaxDepthDecayAlpha      float64 `mapstructure:"adaptiveMaxDepthDecayAlpha"`
	AdaptiveMaxDepthDecayMinSamples int     `mapstructure:"adaptiveMaxDepthDecayMinSamples"`
	AdaptiveMaxDepthDecayMaxSamples int     `mapstructure:"adaptiveMaxDepthDecayMaxSamples"`
	AdaptiveMaxDepthDecayMaxDepth   int     `mapstructure:"adaptiveMaxDepthDecayMaxDepth"`
	// SurpriseRatioThreshold is the multiplicative factor above the surprisal EMA
	// that counts as a sustained novelty burst for plasticity boosting.
	SurpriseRatioThreshold float64 `mapstructure:"surpriseRatioThreshold"`
	// SustainedBurstsRequired is how many consecutive above-threshold observations
	// must occur before the burst boost starts accumulating.
	SustainedBurstsRequired int `mapstructure:"sustainedBurstsRequired"`
	// MaxCapLen limits how many excess burst steps contribute linear boost growth.
	MaxCapLen int `mapstructure:"maxCapLen"`
	// BurstBoostFactor scales the per-step plasticity multiplier once capped.
	BurstBoostFactor float64 `mapstructure:"burstBoostFactor"`
}

type SystemConfig struct {
	BatchSize   int           `mapstructure:"batchSize"`
	BatchWindow time.Duration `mapstructure:"batchWindow"`
	QueueSize   int           `mapstructure:"queueSize"`
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
	Properties: words  48–55  (512 bits; canonical property / forward-transition band)
	Reserved:   words 56–117
	Kernel transport (correlation, residency): words 118–119
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
	System SystemConfig
	Value  ValueConfig

	// TelemetryEnabled controls whether the global telemetry emitter is initialized.
	// When false, all Emit calls resolve to a zero-cost NoopEmitter.
	TelemetryEnabled bool

	// TelemetryEndpoint is the UDP address for experiment/visualizer telemetry (e.g. "127.0.0.1:8258").
	TelemetryEndpoint string

	// TelemetryUniversalBitwiseSlots emits one Backend/UniversalBitwise event per LGP slot per CPU
	// dispatch (very high volume). Use only with viz / short runs.
	TelemetryUniversalBitwiseSlots bool

	// Programs holds raw program source keyed by name, loaded from the
	// `programs:` block of config.yml. These are the in-band programs the
	// substrate kernels execute (affinity fold, popcount, coupling, etc.).
	// Parsing is deferred to pkg/compute/programmer so this package stays
	// free of the programmer IR and avoids an import cycle.
	Programs map[string]string
}

func NewConfig() *Config {
	Cfg = &Config{
		System: SystemConfig{
			BatchSize:   WithDefault(viper.GetInt("system.batchSize"), 10000),
			BatchWindow: time.Duration(WithDefault(viper.GetInt("system.batchWindow"), 500)) * time.Microsecond,
			QueueSize:   WithDefault(viper.GetInt("system.queueSize"), 20000),
		},
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
					Bits:  WithDefault(viper.GetUint64("value.region.properties.bits"), 512),
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
		TelemetryEnabled:               WithDefault(viper.GetBool("telemetry.enabled"), false),
		TelemetryEndpoint:              WithDefault(viper.GetString("telemetry.udp_endpoint"), ""),
		TelemetryUniversalBitwiseSlots: WithDefault(viper.GetBool("telemetry.universal_bitwise_slots"), false),
		Programs:                       loadPrograms(),
	}

	if programLoader != nil && len(Cfg.Programs) > 0 {
		_ = programLoader(Cfg.Programs)
	}

	return Cfg
}

/*
loadPrograms returns the raw text of every entry under the `programs:`
block as a name→source map. Parsing into the programmer representation
happens in pkg/compute/programmer so this package does not pull in that IR.
Missing block yields an empty map so callers can range without nil
checks; zero-length programs are filtered because they indicate a stub
left in the config.
*/
func loadPrograms() map[string]string {
	raw := viper.GetStringMapString("programs")

	if len(raw) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(raw))

	for name, source := range raw {
		if source == "" {
			continue
		}

		out[name] = source
	}

	return out
}

func WithDefault[T comparable](value, defaultValue T) T {
	var zero T

	if value == zero {
		return defaultValue
	}

	return value
}

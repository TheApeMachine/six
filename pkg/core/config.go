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
)

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
	Bits                      int       `mapstructure:"bits"`
	BucketSize                int       `mapstructure:"bucketSize"`
	ReplicationFactor         int       `mapstructure:"replicationFactor"`
	Alpha                     int       `mapstructure:"alpha"`
	EpochQueries              int       `mapstructure:"epochQueries"`
	Penalty                   float64   `mapstructure:"penalty"`
	SecurityThreshold         float64   `mapstructure:"securityThreshold"`
	BucketSecurityThresholds  []float64 `mapstructure:"bucketSecurityThresholds"`
	ShannonLimit              int       `mapstructure:"shannonLimit"`
	ClusterThreshold          int       `mapstructure:"clusterThreshold"`
	AlignedDecayMultiplier    float64   `mapstructure:"alignedDecayMultiplier"`
	AlignedLearnMultiplier    float64   `mapstructure:"alignedLearnMultiplier"`
	MisalignedDecayMultiplier float64   `mapstructure:"misalignedDecayMultiplier"`
	MisalignedLearnMultiplier float64   `mapstructure:"misalignedLearnMultiplier"`
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
	UnknownProbability              float64 `mapstructure:"unknownProbability"`
	AdditiveSmoothing               float64 `mapstructure:"additiveSmoothing"`
	RecentPenalty                   float64 `mapstructure:"recentPenalty"`
	RecentWindow                    int     `mapstructure:"recentWindow"`
	EditDistance                    int     `mapstructure:"editDistance"`
	EditSimilarity                  float64 `mapstructure:"editSimilarity"`
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
	Words        int                `mapstructure:"words"`
	Bytes        int                `mapstructure:"bytes"`
	NumRotations int                `mapstructure:"num_rotations"`
	Region       ValueRegionConfig  `mapstructure:"region"`
	Opcodes      ValueOpcodesConfig `mapstructure:"opcodes"`
}

/*
ValueRegionConfig holds the configuration for a Value's region.
*/
type ValueRegionConfig struct {
	Tokens   ValueOffsetConfig `mapstructure:"tokens"`
	Affinity ValueOffsetConfig `mapstructure:"affinity"`
	Program  ValueOffsetConfig `mapstructure:"program"`
	Signals  ValueOffsetConfig `mapstructure:"signals"`
	Reserved ValueOffsetConfig `mapstructure:"reserved"`
	Prev     ValueOffsetConfig `mapstructure:"prev"`
	Next     ValueOffsetConfig `mapstructure:"next"`
	ID       ValueOffsetConfig `mapstructure:"id"`

	// Legacy fields kept for backward compatibility during migration.
	// TODO: remove once all consumers are updated.
	State     ValueRegionConfigState `mapstructure:"state"`
	Registers ValueRegistersConfig   `mapstructure:"registers"`
	PC        ValueOffsetConfig      `mapstructure:"pc"`
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
ValueRegionConfigState holds the configuration
for a Value's region state.
*/
type ValueRegionConfigState struct {
	Index       int `mapstructure:"index"`
	Sequence    int `mapstructure:"sequence"`
	Accumulator int `mapstructure:"accumulator"`
}

/*
ValueRegistersConfig holds the configuration
for a Value's registers.
*/
type ValueRegistersConfig struct {
	Start int `mapstructure:"start"`
	Bits  int `mapstructure:"bits"`
	R0    int `mapstructure:"r0"`
	R1    int `mapstructure:"r1"`
	R2    int `mapstructure:"r2"`
	R3    int `mapstructure:"r3"`
	R4    int `mapstructure:"r4"`
	R5    int `mapstructure:"r5"`
	R6    int `mapstructure:"r6"`
	R7    int `mapstructure:"r7"`
	R8    int `mapstructure:"r8"`
	R9    int `mapstructure:"r9"`
	FW    int `mapstructure:"fw"`
	PC    int `mapstructure:"pc"`
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
	System     SystemConfig
	Value      ValueConfig
	Kadabra    KadabraConfig
	MarkovTrie MarkovTrieConfig

	// Firmware holds compiled programs from config.yml, indexed by FirmwareType.
	// Values should write the in-band FirmwareRegister* codes to fw rather than
	// assuming the host enum ordinals are stable.
	Firmware [FirmwareTypePrompt + 1][]uint32

	// StepwiseFirmwareSource holds raw programsStepwise.* YAML for hosts that
	// compile or route stepwise bands (loaded whenever the keys are present).
	StepwiseFirmwareSource [FirmwareTypePrompt + 1]string

	// TelemetryEnabled controls whether the global telemetry emitter is initialized.
	// When false, all Emit calls resolve to a zero-cost NoopEmitter.
	TelemetryEnabled bool

	// TelemetryEndpoint is the UDP address for experiment/visualizer telemetry (e.g. "127.0.0.1:8258").
	TelemetryEndpoint string

	// TelemetryUniversalBitwiseSlots emits one Backend/UniversalBitwise event per LGP slot per CPU
	// tile iteration (very high volume). Use only with viz / short runs.
	TelemetryUniversalBitwiseSlots bool
}

func NewConfig() *Config {
	Cfg = &Config{
		System: SystemConfig{
			BatchSize:   WithDefault(viper.GetInt("system.batchSize"), 10000),
			BatchWindow: time.Duration(WithDefault(viper.GetInt("system.batchWindow"), 500)) * time.Microsecond,
			QueueSize:   WithDefault(viper.GetInt("system.queueSize"), 20000),
		},
		Kadabra: KadabraConfig{
			Bits:                      WithDefault(viper.GetInt("kadabra.bits"), 64),
			BucketSize:                WithDefault(viper.GetInt("kadabra.bucketSize"), 20),
			ReplicationFactor:         WithDefault(viper.GetInt("kadabra.replicationFactor"), 3),
			Alpha:                     WithDefault(viper.GetInt("kadabra.alpha"), 3),
			EpochQueries:              WithDefault(viper.GetInt("kadabra.epochQueries"), 100),
			Penalty:                   WithDefault(viper.GetFloat64("kadabra.penalty"), 0.1),
			SecurityThreshold:         WithDefault(viper.GetFloat64("kadabra.securityThreshold"), 0.5),
			AlignedDecayMultiplier:    WithDefault(viper.GetFloat64("kadabra.alignedDecayMultiplier"), 0.1),
			AlignedLearnMultiplier:    WithDefault(viper.GetFloat64("kadabra.alignedLearnMultiplier"), 1.0),
			MisalignedDecayMultiplier: WithDefault(viper.GetFloat64("kadabra.misalignedDecayMultiplier"), 1.0),
			MisalignedLearnMultiplier: WithDefault(viper.GetFloat64("kadabra.misalignedLearnMultiplier"), 0.1),
			// ShannonLimit is the maximum popcount (set bits) a cluster
			// centroid may reach before it is considered full. At 50% set bits
			// (256/512) a random vector's expected Hamming distance equals that
			// of any real vector — all discrimination is gone. The useful range
			// ends well before that. At 47% (~240/512) there is still enough
			// headroom for incoming vectors to register meaningful distance
			// differences, while staying clear of the zone where distances
			// collapse to noise.
			ShannonLimit: WithDefault(viper.GetInt("kadabra.shannonLimit"), 240),
			// ClusterThreshold is the maximum affinity Hamming distance at
			// which a Value is routed to an existing trie cluster. Values further
			// than this from every existing cluster spawn a new trie.
			//
			// 512 total affinity bits. A threshold of 192 (~37%) means two vectors
			// must share at least 63% of their bits to be considered "same cluster".
			// This is intentionally generous during bootstrapping — the Field's
			// eigenmode detection handles finer-grained splitting once enough data
			// has arrived.
			ClusterThreshold: WithDefault(viper.GetInt("kadabra.clusterThreshold"), 192),
		},
		MarkovTrie: MarkovTrieConfig{
			DecayFactor:                    WithDefault(viper.GetFloat64("markovtrie.decayFactor"), 0.995),
			EndToken:                       WithDefault(viper.GetString("markovtrie.endToken"), "$"),
			MaximumPathLength:              WithDefault(viper.GetInt("markovtrie.maximumPathLength"), 5),
			InterpolationSuffixDepth:       WithDefault(viper.GetInt("markovtrie.interpolationSuffixDepth"), 4),
			ClassificationContext:          WithDefault(viper.GetInt("markovtrie.classificationContext"), 3),
			CoOccurrenceWindow:             WithDefault(viper.GetInt("markovtrie.coOccurrenceWindow"), 2),
			PruneInterval:                  WithDefault(viper.GetInt("markovtrie.pruneInterval"), 10),
			PruneMinimumCount:              WithDefault(viper.GetFloat64("markovtrie.pruneMinimumCount"), 0.05),
			ReplayLength:                   WithDefault(viper.GetInt("markovtrie.replayLength"), 10),
			ReplayThreshold:                WithDefault(viper.GetFloat64("markovtrie.replayThreshold"), 85),
			UnknownProbability:             WithDefault(viper.GetFloat64("markovtrie.unknownProbability"), 0.001),
			AdditiveSmoothing:              WithDefault(viper.GetFloat64("markovtrie.additiveSmoothing"), 0.1),
			RecentPenalty:                  WithDefault(viper.GetFloat64("markovtrie.recentPenalty"), 0.5),
			RecentWindow:                   WithDefault(viper.GetInt("markovtrie.recentWindow"), 3),
			EditDistance:                   WithDefault(viper.GetInt("markovtrie.editDistance"), 1),
			EditSimilarity:                 WithDefault(viper.GetFloat64("markovtrie.editSimilarity"), 0.95),
			SymbolMinimumTotal:             WithDefault(viper.GetInt("markovtrie.symbolMinimumTotal"), 2),
			SymbolMinimumScore:             WithDefault(viper.GetFloat64("markovtrie.symbolMinimumScore"), 1.5),
			SymbolLimit:                    WithDefault(viper.GetInt("markovtrie.symbolLimit"), 50),
			BaselineLearningRate:           WithDefault(viper.GetFloat64("markovtrie.baselineLearningRate"), 0.1),
			MaxLearningRate:                WithDefault(viper.GetFloat64("markovtrie.maxLearningRate"), 1.0),
			SurprisalScaleBits:             WithDefault(viper.GetFloat64("markovtrie.surprisalScaleBits"), 2.0),
			ConceptLabelPrefix:             WithDefault(viper.GetString("markovtrie.conceptLabelPrefix"), "Concept_"),
			UnsupervisedConfidence:         WithDefault(viper.GetFloat64("markovtrie.unsupervisedConfidence"), 50.0),
			ExperienceEmptyLabel:           WithDefault(viper.GetString("markovtrie.experienceEmptyLabel"), "None"),
			EpisodicCapacity:               WithDefault(viper.GetInt("markovtrie.episodicCapacity"), 1000),
			EpisodicNeighborLimit:          WithDefault(viper.GetInt("markovtrie.episodicNeighborLimit"), 16),
			EpisodicRecencyWeight:          WithDefault(viper.GetFloat64("markovtrie.episodicRecencyWeight"), 0.25),
			EpisodicBlendWeight:            WithDefault(viper.GetFloat64("markovtrie.episodicBlendWeight"), 0.35),
			InitialConceptCounter:          WithDefault(viper.GetInt("markovtrie.initialConceptCounter"), 1),
			BPEEndOfWordToken:              WithDefault(viper.GetString("markovtrie.bpeEndOfWordToken"), "</w>"),
			BPEPairDelimiter:               WithDefault(viper.GetString("markovtrie.bpePairDelimiter"), "\x00"),
			PredictExperienceSurprisalBits: WithDefault(viper.GetFloat64("markovtrie.predictExperienceSurprisalBits"), 1.0),
			Temperature:                    WithDefault(viper.GetFloat64("markovtrie.temperature"), 0.7),
			// AdaptiveEMAAlpha (0.05): the smoothing factor for all exponential
			// moving averages (surprisal, entropy, episodic quality, growth rate).
			// At alpha=0.05, the effective window is ~1/0.05 = 20 observations,
			// meaning the EMA reflects roughly the last 20 tokens' worth of signal.
			//
			// This balances responsiveness against stability: the system needs to
			// detect domain shifts within ~50-100 tokens (a paragraph) but must not
			// whipsaw on individual high-surprisal tokens (typos, rare words).
			//
			// Sensitivity: raising to 0.2 makes all adaptive parameters jittery --
			// a single outlier surprisal value can swing the decay factor by 0.01,
			// causing visible oscillation in prediction quality. Lowering below 0.01
			// makes adaptation so sluggish that domain shifts take hundreds of tokens
			// to register, defeating the purpose of online tuning.
			AdaptiveEMAAlpha: WithDefault(viper.GetFloat64("markovtrie.adaptiveEMAAlpha"), 0.05),
			// AdaptiveMinSamples (50): the minimum number of surprisal observations
			// before any adaptive parameter is allowed to deviate from its base
			// value. This is a cold-start guard -- with fewer than 50 samples, the
			// EMA estimates are dominated by initialization bias and the variance
			// estimate is unreliable (sqrt of a noisy variance can be off by 2-3x).
			//
			// 50 was chosen as ~2.5x the EMA effective window (20). By that point,
			// the exponential weighting has decayed the initial seed value to <10%
			// influence, and the variance estimate has seen enough samples for its
			// own EMA to stabilize.
			//
			// Sensitivity: reducing to <20 allows the adaptive decay factor to
			// activate before the surprisal EMA is calibrated, producing erratic
			// early decay that can prune useful nodes. Increasing beyond ~200 delays
			// adaptation so long that the system runs on static defaults for the
			// first several paragraphs, losing the benefit of early calibration.
			AdaptiveMinSamples: WithDefault(viper.GetInt("markovtrie.adaptiveMinSamples"), 50),
			AdaptiveMaxSamples: WithDefault(viper.GetInt("markovtrie.adaptiveMaxSamples"), 1000),
			// AdaptiveMaxDepth (8): the maximum suffix depth tracked for adaptive
			// interpolation weighting. 8 corresponds to 8-gram context, which is the
			// practical ceiling for Markov models on natural language -- beyond depth
			// 8, the trie becomes extremely sparse and hit rates drop below noise
			// levels (~1-2%), so tracking deeper depths wastes memory on counters
			// that never accumulate meaningful statistics.
			//
			// Sensitivity: reducing to 4 blinds the system to long-range patterns
			// (e.g. repeated phrases, code indentation). Increasing to 16 doubles
			// the per-node tracking arrays with no measurable prediction improvement,
			// and the depthDecay factor (0.99) would need to be raised to prevent
			// deep counters from decaying to zero before accumulating any hits.
			AdaptiveMaxDepth: WithDefault(viper.GetInt("markovtrie.adaptiveMaxDepth"), 8),
			// AdaptiveMaxDepthDecay (0.99): multiplicative decay applied to all depth-hit
			// counters on every observation. This creates a soft sliding window
			// so that the interpolation weights track the RECENT productivity of
			// each depth rather than lifetime averages.
			//
			// At 0.99, the effective half-life is ~69 observations (ln2/0.01),
			// meaning depth statistics from ~70 tokens ago carry half the weight
			// of current observations. This matches the typical paragraph length
			// where context quality shifts (e.g. switching from prose to code).
			//
			// Sensitivity: raising to 0.999 (half-life ~693) makes depth weights
			// nearly static, unable to track intra-document shifts. Lowering to
			// 0.95 (half-life ~14) makes weights hyper-reactive, thrashing
			// between depths on every few tokens and preventing stable
			// interpolation.
			AdaptiveMaxDepthDecay:           WithDefault(viper.GetFloat64("markovtrie.adaptiveMaxDepthDecay"), 0.99),
			AdaptiveMaxDepthDecayAlpha:      WithDefault(viper.GetFloat64("markovtrie.adaptiveMaxDepthDecayAlpha"), 0.05),
			AdaptiveMaxDepthDecayMinSamples: WithDefault(viper.GetInt("markovtrie.adaptiveMaxDepthDecayMinSamples"), 50),
			AdaptiveMaxDepthDecayMaxSamples: WithDefault(viper.GetInt("markovtrie.adaptiveMaxDepthDecayMaxSamples"), 1000),
			AdaptiveMaxDepthDecayMaxDepth:   WithDefault(viper.GetInt("markovtrie.adaptiveMaxDepthDecayMaxDepth"), 8),
		},
		Value: ValueConfig{
			Words:        WithDefault(viper.GetInt("value.words"), 128),
			Bytes:        WithDefault(viper.GetInt("value.bytes"), 1024),
			NumRotations: WithDefault(viper.GetInt("value.num_rotations"), 16),
			Region: ValueRegionConfig{
				Tokens: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.tokens.start"), 0),
					Bits:  WithDefault(viper.GetUint64("value.region.tokens.bits"), 512),
				},
				ID: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.id.start"), 8),
					Bits:  WithDefault(viper.GetUint64("value.region.id.bits"), 64),
				},
				Prev: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.prev.start"), 125),
					Bits:  WithDefault(viper.GetUint64("value.region.prev.bits"), 64),
				},
				Next: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.next.start"), 126),
					Bits:  WithDefault(viper.GetUint64("value.region.next.bits"), 64),
				},
				State: ValueRegionConfigState{
					Index:       WithDefault(viper.GetInt("value.region.state.index"), 32),
					Sequence:    WithDefault(viper.GetInt("value.region.state.sequence"), 33),
					Accumulator: WithDefault(viper.GetInt("value.region.state.accumulator"), 34),
				},
				Affinity: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.affinity.start"), 8),
					Bits:  WithDefault(viper.GetUint64("value.region.affinity.bits"), 512),
				},
				Registers: ValueRegistersConfig{
					Start: WithDefault(viper.GetInt("value.region.registers.start"), 27),
					Bits:  WithDefault(viper.GetInt("value.region.registers.bits"), 768),
					R0:    WithDefault(viper.GetInt("value.region.registers.r0"), 27),
					R1:    WithDefault(viper.GetInt("value.region.registers.r1"), 28),
					R2:    WithDefault(viper.GetInt("value.region.registers.r2"), 29),
					R3:    WithDefault(viper.GetInt("value.region.registers.r3"), 30),
					R4:    WithDefault(viper.GetInt("value.region.registers.r4"), 31),
					R5:    WithDefault(viper.GetInt("value.region.registers.r5"), 32),
					R6:    WithDefault(viper.GetInt("value.region.registers.r6"), 33),
					R7:    WithDefault(viper.GetInt("value.region.registers.r7"), 34),
					R8:    WithDefault(viper.GetInt("value.region.registers.r8"), 35),
					R9:    WithDefault(viper.GetInt("value.region.registers.r9"), 36),
					FW:    WithDefault(viper.GetInt("value.region.registers.fw"), 37),
					PC:    WithDefault(viper.GetInt("value.region.registers.pc"), 38),
				},
				Program: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.program.start"), 16),
					Bits:  WithDefault(viper.GetUint64("value.region.program.bits"), 512),
				},
				Signals: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.signals.start"), 24),
					Bits:  WithDefault(viper.GetUint64("value.region.signals.bits"), 512),
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
	}

	return Cfg
}

func WithDefault[T comparable](value, defaultValue T) T {
	if value != defaultValue {
		return defaultValue
	}

	return value
}

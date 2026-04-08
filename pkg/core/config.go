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

	Tokens:   words  0– 7  (512 bits)
	Program:  words  8–15  (512 bits)
	Signals:  words 16–23  (512 bits)
	Context:  words 24–31  (512 bits)
	Gradient: words 32–39  (512 bits)
	Meta:     words 40–47  (512 bits)
	Reserved: words 48–117
	Kernel transport (correlation, residency): words 118–119
	Prev:     word  120
	Next:     word  121
	ID:       word  122
	Affinity: words 123–127 (257 bits, Fermat prime width)
*/
type ValueRegionConfig struct {
	Tokens   ValueOffsetConfig `mapstructure:"tokens"`
	Program  ValueOffsetConfig `mapstructure:"program"`
	Signals  ValueOffsetConfig `mapstructure:"signals"`
	Context  ValueOffsetConfig `mapstructure:"context"`
	Gradient ValueOffsetConfig `mapstructure:"gradient"`
	Meta     ValueOffsetConfig `mapstructure:"meta"`
	Prev     ValueOffsetConfig `mapstructure:"prev"`
	Next     ValueOffsetConfig `mapstructure:"next"`
	ID       ValueOffsetConfig `mapstructure:"id"`
	Affinity ValueOffsetConfig `mapstructure:"affinity"`
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
			Bits:              WithDefault(viper.GetInt("kadabra.bits"), 64),
			BucketSize:        WithDefault(viper.GetInt("kadabra.bucketSize"), 20),
			ReplicationFactor: WithDefault(viper.GetInt("kadabra.replicationFactor"), 3),
			MaxMeshPeers:      WithDefault(viper.GetInt("kadabra.maxMeshPeers"), 4096),
			Alpha:             WithDefault(viper.GetInt("kadabra.alpha"), 3),
			EpochQueries:      WithDefault(viper.GetInt("kadabra.epochQueries"), 100),
			Penalty:           WithDefault(viper.GetFloat64("kadabra.penalty"), 0.1),
			SecurityThreshold: WithDefault(viper.GetFloat64("kadabra.securityThreshold"), 0.5),
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
			BeamWidth:                      WithDefault(viper.GetInt("markovtrie.beamWidth"), 3),
			UnknownProbability:             WithDefault(viper.GetFloat64("markovtrie.unknownProbability"), 0.001),
			AdditiveSmoothing:              WithDefault(viper.GetFloat64("markovtrie.additiveSmoothing"), 0.1),
			RecentPenalty:                  WithDefault(viper.GetFloat64("markovtrie.recentPenalty"), 0.5),
			RecentWindow:                   WithDefault(viper.GetInt("markovtrie.recentWindow"), 3),
			EditDistance:                   WithDefault(viper.GetInt("markovtrie.editDistance"), 1),
			EditSimilarity:                 WithDefault(viper.GetFloat64("markovtrie.editSimilarity"), 0.95),
			NgramConfidenceFloor:           WithDefault(viper.GetFloat64("markovtrie.ngramConfidenceFloor"), 0.35),
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
			SurpriseRatioThreshold:          WithDefault(viper.GetFloat64("markovtrie.surpriseRatioThreshold"), 2.0),
			SustainedBurstsRequired:         WithDefault(viper.GetInt("markovtrie.sustainedBurstsRequired"), 3),
			MaxCapLen:                       WithDefault(viper.GetInt("markovtrie.maxCapLen"), 8),
			BurstBoostFactor:                WithDefault(viper.GetFloat64("markovtrie.burstBoostFactor"), 0.12),
		},
		Value: ValueConfig{
			Word:         WithDefault(viper.GetInt("value.word"), 64),
			Words:        WithDefault(viper.GetInt("value.words"), 128),
			Bytes:        WithDefault(viper.GetInt("value.bytes"), 1024),
			NumRotations: WithDefault(viper.GetInt("value.num_rotations"), 16),
			Region: ValueRegionConfig{
				Tokens: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.tokens.start"), 0),
					Bits:  WithDefault(viper.GetUint64("value.region.tokens.bits"), 512),
				},
				Program: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.program.start"), 8),
					Bits:  WithDefault(viper.GetUint64("value.region.program.bits"), 512),
				},
				Signals: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.signals.start"), 16),
					Bits:  WithDefault(viper.GetUint64("value.region.signals.bits"), 512),
				},
				Context: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.context.start"), 24),
					Bits:  WithDefault(viper.GetUint64("value.region.context.bits"), 512),
				},
				Gradient: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.gradient.start"), 32),
					Bits:  WithDefault(viper.GetUint64("value.region.gradient.bits"), 512),
				},
				Meta: ValueOffsetConfig{
					Start: WithDefault(viper.GetInt("value.region.meta.start"), 40),
					Bits:  WithDefault(viper.GetUint64("value.region.meta.bits"), 512),
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

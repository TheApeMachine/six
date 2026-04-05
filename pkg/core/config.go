package core

import (
	"hash/fnv"
	"os"
	"time"

	"github.com/spf13/viper"
)

/*
stableNodeID derives a deterministic 64-bit node identity from the host's
hostname. This ensures a node's position in the Kademlia DHT is stable
across process restarts on the same host, and distinct across machines,
without depending on transient workload frames or PIDs.
*/
func stableNodeID() uint64 {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "six-node"
	}
	h := fnv.New64a()
	h.Write([]byte(hostname))
	return h.Sum64()
}

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
	Alpha                    int       `mapstructure:"alpha"`
	EpochQueries             int       `mapstructure:"epochQueries"`
	Penalty                  float64   `mapstructure:"penalty"`
	SecurityThreshold        float64   `mapstructure:"securityThreshold"`
	BucketSecurityThresholds []float64 `mapstructure:"bucketSecurityThresholds"`
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
	Words   int                `mapstructure:"words"`
	Bytes   int                `mapstructure:"bytes"`
	Region  ValueRegionConfig  `mapstructure:"region"`
	Opcodes ValueOpcodesConfig `mapstructure:"opcodes"`
}

/*
ValueRegionConfig holds the configuration for a Value's region.
*/
type ValueRegionConfig struct {
	Tokens    ValueOffsetConfig      `mapstructure:"tokens"`
	ID        ValueOffsetConfig      `mapstructure:"id"`
	Prev      ValueOffsetConfig      `mapstructure:"prev"`
	Next      ValueOffsetConfig      `mapstructure:"next"`
	State     ValueRegionConfigState `mapstructure:"state"`
	Affinity  ValueOffsetConfig      `mapstructure:"affinity"`
	Registers ValueRegistersConfig   `mapstructure:"registers"`
	PC        ValueOffsetConfig      `mapstructure:"pc"`
	Program   ValueOffsetConfig      `mapstructure:"program"`
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
	System  SystemConfig
	Value   ValueConfig
	Kadabra KadabraConfig

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
			BatchSize:   viper.GetInt("system.batchSize"),
			BatchWindow: time.Duration(viper.GetInt("system.batchWindow")) * time.Microsecond,
			QueueSize:   viper.GetInt("system.queueSize"),
		},
		Kadabra: func() KadabraConfig {
			kadabraCfg := KadabraConfig{
				Bits:              viper.GetInt("kadabra.bits"),
				BucketSize:        viper.GetInt("kadabra.bucketSize"),
				ReplicationFactor: viper.GetInt("kadabra.replicationFactor"),
				Alpha:             viper.GetInt("kadabra.alpha"),
				EpochQueries:      viper.GetInt("kadabra.epochQueries"),
				Penalty:           viper.GetFloat64("kadabra.penalty"),
				SecurityThreshold: viper.GetFloat64("kadabra.securityThreshold"),
			}

			var bucketThresholds []float64

			if err := viper.UnmarshalKey("kadabra.bucketSecurityThresholds", &bucketThresholds); err == nil {
				kadabraCfg.BucketSecurityThresholds = bucketThresholds
			}

			return kadabraCfg
		}(),
		Value: ValueConfig{
			Words: viper.GetInt("value.words"),
			Bytes: viper.GetInt("value.bytes"),
			Region: ValueRegionConfig{
				Tokens: ValueOffsetConfig{
					Start: viper.GetInt("value.region.tokens.start"),
					Bits:  viper.GetUint64("value.region.tokens.bits"),
				},
				ID: ValueOffsetConfig{
					Start: viper.GetInt("value.region.id.start"),
					Bits:  viper.GetUint64("value.region.id.bits"),
				},
				Prev: ValueOffsetConfig{
					Start: viper.GetInt("value.region.prev.start"),
					Bits:  viper.GetUint64("value.region.prev.bits"),
				},
				Next: ValueOffsetConfig{
					Start: viper.GetInt("value.region.next.start"),
					Bits:  viper.GetUint64("value.region.next.bits"),
				},
				State: ValueRegionConfigState{
					Index:       viper.GetInt("value.region.state.index"),
					Sequence:    viper.GetInt("value.region.state.sequence"),
					Accumulator: viper.GetInt("value.region.state.accumulator"),
				},
				Affinity: ValueOffsetConfig{
					Start: viper.GetInt("value.region.affinity.start"),
					Bits:  viper.GetUint64("value.region.affinity.bits"),
				},
				Registers: ValueRegistersConfig{
					Start: viper.GetInt("value.region.registers.start"),
					Bits:  viper.GetInt("value.region.registers.bits"),
					R0:    viper.GetInt("value.region.registers.r0"),
					R1:    viper.GetInt("value.region.registers.r1"),
					R2:    viper.GetInt("value.region.registers.r2"),
					R3:    viper.GetInt("value.region.registers.r3"),
					R4:    viper.GetInt("value.region.registers.r4"),
					R5:    viper.GetInt("value.region.registers.r5"),
					R6:    viper.GetInt("value.region.registers.r6"),
					R7:    viper.GetInt("value.region.registers.r7"),
					R8:    viper.GetInt("value.region.registers.r8"),
					R9:    viper.GetInt("value.region.registers.r9"),
					FW:    viper.GetInt("value.region.registers.fw"),
					PC:    viper.GetInt("value.region.registers.pc"),
				},
				Program: ValueOffsetConfig{
					Start: viper.GetInt("value.region.program.start"),
					Bits:  viper.GetUint64("value.region.program.bits"),
				},
			},
			Opcodes: ValueOpcodesConfig{
				False:    viper.GetString("value.opcodes.false"),
				And:      viper.GetString("value.opcodes.and"),
				AandNotB: viper.GetString("value.opcodes.aandnotb"),
				A:        viper.GetString("value.opcodes.a"),
				NotAandB: viper.GetString("value.opcodes.notandb"),
				B:        viper.GetString("value.opcodes.b"),
				XOR:      viper.GetString("value.opcodes.xor"),
				OR:       viper.GetString("value.opcodes.or"),
				NOR:      viper.GetString("value.opcodes.nor"),
				XNOR:     viper.GetString("value.opcodes.xnor"),
				NOTB:     viper.GetString("value.opcodes.notb"),
				IFBTHENA: viper.GetString("value.opcodes.ifbthena"),
			},
		},
		TelemetryEnabled:               viper.GetBool("telemetry.enabled"),
		TelemetryEndpoint:              viper.GetString("telemetry.udp_endpoint"),
		TelemetryUniversalBitwiseSlots: viper.GetBool("telemetry.universal_bitwise_slots"),
	}

	return Cfg
}

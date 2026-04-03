package core

import (
	"hash/fnv"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/theapemachine/six/pkg/errnie"
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

type ControlPlaneConfig struct {
	K        int               `mapstructure:"k"`
	Alpha    int               `mapstructure:"alpha"`
	Affinity ValueOffsetConfig `mapstructure:"affinity"`
	// NodeID is the stable identity for this node in the Kademlia DHT.
	// When zero (default), a deterministic ID is derived from the host
	// identity (hostname + PID) so the node doesn't inherit its position
	// in the routing table from the first transient workload frame.
	NodeID uint64 `mapstructure:"nodeId"`
}

type SystemConfig struct {
	BatchSize   int           `mapstructure:"batchSize"`
	BatchWindow time.Duration `mapstructure:"batchWindow"`
	QueueSize   int           `mapstructure:"queueSize"`

	// EvolutionBatchWindow is a lower bound on ingress coalescing time
	// (see pkg/compute.Backend.gatherCoalesceDuration). Prompt Values block
	// Machine.Read on settle polling; sub-millisecond BatchWindow alone can
	// close the batch before a mate is queued. Set this above
	// vm.promptSettleDeadline (e.g. 75ms) for eval / paper runs.
	EvolutionBatchWindow time.Duration `mapstructure:"evolutionBatchWindow"`

	// HolisticChunkBits is the chunk size in bits for chunked Hamming similarity
	// (holistic emission path and exploit scoring).
	HolisticChunkBits int `mapstructure:"holisticChunkBits"`

	// HolisticHammingMax is the maximum normalized Hamming distance per chunk
	// (0–1) that still counts as a strong holistic match for emission.
	HolisticHammingMax float64 `mapstructure:"holisticHammingMax"`

	// MapElitesInjectionRate is the probability [0,1] of copying the program
	// band from a sampled elite bin before crossover.
	MapElitesInjectionRate float64 `mapstructure:"mapElitesInjectionRate"`

	// MapElitesGridShift number of high bits of Affinity XOR-folded into a bin.
	MapElitesGridShift uint `mapstructure:"mapElitesGridShift"`

	// TokenSettleMaxPasses bounds extra UniversalBitwise sweeps until the token
	// region stabilizes. Non-positive values are replaced with
	// DefaultTokenSettleMaxPasses at load time.
	TokenSettleMaxPasses int `mapstructure:"tokenSettleMaxPasses"`

	// TokenSettleEpsilonBits: max Hamming distance in the token region across
	// two consecutive passes to treat the frame as settled (0 = exact).
	TokenSettleEpsilonBits int `mapstructure:"tokenSettleEpsilonBits"`

	// ThermodynamicEnergyWord is the Value word index holding uint16-like energy
	// (low 16 bits). -1 defaults to registers.r9 from config.
	ThermodynamicEnergyWord int `mapstructure:"thermodynamicEnergyWord"`

	// ThermodynamicBirthEnergy seeds new Values and emitted children.
	ThermodynamicBirthEnergy int `mapstructure:"thermodynamicBirthEnergy"`

	// ThermodynamicDecayDelta subtracted from energy each batch touch.
	ThermodynamicDecayDelta int `mapstructure:"thermodynamicDecayDelta"`

	// ThermodynamicEmitGain adds energy to parents when signal emission succeeds.
	ThermodynamicEmitGain int `mapstructure:"thermodynamicEmitGain"`

	// SleepMaxPairs caps how many random frame pairs sleep consolidation tries
	// per tick. Non-positive values default to 4 at load time.
	SleepMaxPairs int `mapstructure:"sleepMaxPairs"`
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
	System       SystemConfig
	Value        ValueConfig
	ControlPlane ControlPlaneConfig

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
			EvolutionBatchWindow: time.Duration(
				viper.GetInt("system.evolutionBatchWindow"),
			) * time.Microsecond,
			HolisticChunkBits:       viper.GetInt("system.holisticChunkBits"),
			HolisticHammingMax:      viper.GetFloat64("system.holisticHammingMax"),
			MapElitesInjectionRate:  viper.GetFloat64("system.mapElitesInjectionRate"),
			MapElitesGridShift:      uint(viper.GetInt("system.mapElitesGridShift")),
			TokenSettleMaxPasses:    viper.GetInt("system.tokenSettleMaxPasses"),
			TokenSettleEpsilonBits:  viper.GetInt("system.tokenSettleEpsilonBits"),
			ThermodynamicEnergyWord: viper.GetInt("system.thermodynamicEnergyWord"),
			ThermodynamicBirthEnergy: viper.GetInt(
				"system.thermodynamicBirthEnergy",
			),
			ThermodynamicDecayDelta: viper.GetInt("system.thermodynamicDecayDelta"),
			ThermodynamicEmitGain:   viper.GetInt("system.thermodynamicEmitGain"),
			SleepMaxPairs:           viper.GetInt("system.sleepMaxPairs"),
		},
		ControlPlane: ControlPlaneConfig{
			K:      viper.GetInt("controlplane.k"),
			Alpha:  viper.GetInt("controlplane.alpha"),
			NodeID: viper.GetUint64("controlplane.nodeId"),
			Affinity: ValueOffsetConfig{
				Start: viper.GetInt("controlplane.affinity.start"),
				Bits:  viper.GetUint64("controlplane.affinity.bits"),
			},
		},
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
		Firmware: [FirmwareTypePrompt + 1][]uint32{
			FirmwareTypeLearn: Cfg.compileAndAssign(
				FirmwareTypeLearn, viper.GetString("programs.learn"),
			),
			FirmwareTypeBootloader: Cfg.compileAndAssign(
				FirmwareTypeBootloader, viper.GetString("programs.bootloader")),
			FirmwareTypeTombstone: Cfg.compileAndAssign(
				FirmwareTypeTombstone, viper.GetString("programs.tombstone"),
			),
			FirmwareTypeViral: Cfg.compileAndAssign(
				FirmwareTypeViral, viper.GetString("programs.viral"),
			),
			FirmwareTypeBuild: Cfg.compileAndAssign(
				FirmwareTypeBuild, viper.GetString("programs.build"),
			),
			FirmwareTypeQuery: Cfg.compileAndAssign(
				FirmwareTypeQuery, viper.GetString("programs.query"),
			),
			FirmwareTypePrompt: Cfg.compileAndAssign(
				FirmwareTypePrompt, viper.GetString("programs.prompt"),
			),
		},
	}

	if Cfg.ControlPlane.NodeID == 0 {
		Cfg.ControlPlane.NodeID = stableNodeID()
		errnie.Info(
			"core.config: controlplane.nodeId derived from host identity",
			"nodeId", Cfg.ControlPlane.NodeID,
		)
	}

	if Cfg.ControlPlane.K < 1 {
		errnie.Info(
			"core.config: controlplane.k defaulted",
			"was", Cfg.ControlPlane.K,
			"now", 20,
		)
		Cfg.ControlPlane.K = 20
	}

	if Cfg.ControlPlane.Alpha < 1 {
		errnie.Info(
			"core.config: controlplane.alpha defaulted",
			"was", Cfg.ControlPlane.Alpha,
			"now", 3,
		)
		Cfg.ControlPlane.Alpha = 3
	}

	if Cfg.ControlPlane.Affinity.Bits == 0 {
		errnie.Info(
			"core.config: controlplane.affinity.bits defaulted",
			"was", 0,
			"now", uint64(1),
		)
		Cfg.ControlPlane.Affinity.Bits = 1
	}

	if Cfg.TelemetryEnabled && strings.TrimSpace(Cfg.TelemetryEndpoint) == "" {
		Cfg.TelemetryEndpoint = "127.0.0.1:8258"
	}

	if Cfg.System.HolisticChunkBits <= 0 {
		Cfg.System.HolisticChunkBits = 512
	}

	if Cfg.System.HolisticHammingMax <= 0 {
		Cfg.System.HolisticHammingMax = 0.45
	}

	if Cfg.System.MapElitesGridShift == 0 {
		Cfg.System.MapElitesGridShift = 8
	}

	if Cfg.System.MapElitesInjectionRate < 0 {
		Cfg.System.MapElitesInjectionRate = 0
	}

	if Cfg.System.MapElitesInjectionRate > 1 {
		Cfg.System.MapElitesInjectionRate = 1
	}

	if Cfg.System.MapElitesInjectionRate <= 0 {
		Cfg.System.MapElitesInjectionRate = 0.08
	}

	if Cfg.System.TokenSettleMaxPasses <= 0 {
		Cfg.System.TokenSettleMaxPasses = DefaultTokenSettleMaxPasses
	}

	if Cfg.System.TokenSettleEpsilonBits < 0 {
		Cfg.System.TokenSettleEpsilonBits = 0
	}

	if Cfg.System.ThermodynamicBirthEnergy < 0 {
		Cfg.System.ThermodynamicBirthEnergy = 0
	}

	if Cfg.System.ThermodynamicDecayDelta < 0 {
		Cfg.System.ThermodynamicDecayDelta = 0
	}

	if Cfg.System.ThermodynamicEmitGain < 0 {
		Cfg.System.ThermodynamicEmitGain = 0
	}

	if Cfg.System.SleepMaxPairs <= 0 {
		Cfg.System.SleepMaxPairs = 4
	}

	Cfg.StepwiseFirmwareSource[FirmwareTypeLearn] = viper.GetString(
		"programsStepwise.learn",
	)
	Cfg.StepwiseFirmwareSource[FirmwareTypeBootloader] = viper.GetString(
		"programsStepwise.bootloader",
	)
	Cfg.StepwiseFirmwareSource[FirmwareTypeTombstone] = viper.GetString(
		"programsStepwise.tombstone",
	)
	Cfg.StepwiseFirmwareSource[FirmwareTypeViral] = viper.GetString(
		"programsStepwise.viral",
	)
	Cfg.StepwiseFirmwareSource[FirmwareTypeBuild] = viper.GetString(
		"programsStepwise.build",
	)
	Cfg.StepwiseFirmwareSource[FirmwareTypeQuery] = viper.GetString(
		"programsStepwise.query",
	)
	Cfg.StepwiseFirmwareSource[FirmwareTypePrompt] = viper.GetString(
		"programsStepwise.prompt",
	)

	return Cfg
}

/*
LoadFirmware compiles all programs from the config's `programs` section
into Cfg.Firmware. Must be called after viper has loaded config.
*/
func (config *Config) compileAndAssign(ft FirmwareType, src string) []uint32 {
	program, err := CompileFunc(src)

	if err != nil {
		return nil
	}

	return program
}

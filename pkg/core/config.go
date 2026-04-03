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

	// StepwiseUniversalBitwise enables per-frame routing: when the program band
	// starts with stepwise.PackEmbeddedHeader, execution uses the fixed-step
	// executor; otherwise the frame uses in-band 32-bit LGP slots only. GPU
	// kernels are unchanged.
	StepwiseUniversalBitwise bool `mapstructure:"stepwiseUniversalBitwise"`

	// ProgramEvolution runs HomologousCrossover on adjacent pairs within each
	// executeBatch group after UniversalBitwise (pkg/compute/firmware).
	ProgramEvolution bool `mapstructure:"programEvolution"`

	// EvolutionBatchWindow is an optional lower bound on ingress coalescing time
	// when ProgramEvolution is true (see pkg/compute.Backend.gatherBatch). Prompt
	// Values block Machine.Read on settle polling; sub-millisecond BatchWindow
	// closes the batch before a mate is queued, so crossover never runs. Set
	// this above vm.promptSettleDeadline (e.g. 75ms) for eval / paper runs.
	EvolutionBatchWindow time.Duration `mapstructure:"evolutionBatchWindow"`
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

	// StepwiseFirmwareSource holds raw programsStepwise.* YAML when
	// system.stepwiseUniversalBitwise is true. primitive.installFirmware compiles
	// these via pkg/compute/stepwise at install time (avoid core→stepwise import cycle).
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
			StepwiseUniversalBitwise: viper.GetBool(
				"system.stepwiseUniversalBitwise",
			),
			ProgramEvolution: viper.GetBool("system.programEvolution"),
			EvolutionBatchWindow: time.Duration(
				viper.GetInt("system.evolutionBatchWindow"),
			) * time.Microsecond,
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

	if Cfg.System.StepwiseUniversalBitwise {
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
	}

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

package core

import (
	"fmt"
	"sort"

	"github.com/spf13/viper"
)

type ValueConfig struct {
	Words   int                `mapstructure:"words"`
	Region  ValueRegionConfig  `mapstructure:"region"`
	Opcodes ValueOpcodesConfig `mapstructure:"opcodes"`
}

type ValueRegionConfig struct {
	Tokens    ValueOffsetConfig      `mapstructure:"tokens"`
	ID        ValueOffsetConfig      `mapstructure:"id"`
	Prev      ValueOffsetConfig      `mapstructure:"prev"`
	Next      ValueOffsetConfig      `mapstructure:"next"`
	State     ValueRegionConfigState `mapstructure:"state"`
	Affinity  ValueOffsetConfig      `mapstructure:"affinity"`
	Gossip    ValueOffsetConfig      `mapstructure:"gossip"`
	TTL       ValueOffsetConfig      `mapstructure:"ttl"`
	Registers ValueRegistersConfig   `mapstructure:"registers"`
	PC        ValueOffsetConfig      `mapstructure:"pc"`
	Program   ValueOffsetConfig      `mapstructure:"program"`
}

type ValueOffsetConfig struct {
	Start int    `mapstructure:"start"`
	Bits  uint64 `mapstructure:"bits"`
}

type ValueRegionConfigState struct {
	Index       int `mapstructure:"index"`
	Sequence    int `mapstructure:"sequence"`
	Accumulator int `mapstructure:"accumulator"`
}

type ValueRegistersConfig struct {
	Start int `mapstructure:"start"`
	Bits  int `mapstructure:"bits"`
	R0    int `mapstructure:"r0"`
	R1    int `mapstructure:"r1"`
	R2    int `mapstructure:"r2"`
	R3    int `mapstructure:"r3"`
	R4    int `mapstructure:"r4"`
	R5    int `mapstructure:"r5"`
	FW    int `mapstructure:"fw"`
	PC    int `mapstructure:"pc"`
}

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

// V holds the loaded Value configuration. Populated by LoadValueConfig.
var vCfg ValueConfig

// LoadValueConfig reads the "value" section from viper into V.
func LoadValueConfig() error {
	return viper.UnmarshalKey("value", &vCfg)
}

/*
Cfg is the global config accessor. All reads go through typed methods
that error when the requested key is absent, so callers never silently
operate on zero-values from a missing key.
*/
var Cfg = &Config{
	StateIndex:       vCfg.Region.State.Index,
	StateSequence:    vCfg.Region.State.Sequence,
	StateAccumulator: vCfg.Region.State.Accumulator,
	TokenIndex:       vCfg.Region.Tokens.Start,
	TokenBits:        vCfg.Region.Tokens.Bits,
	ValueID:          vCfg.Region.ID.Start,
	PreviousID:       vCfg.Region.Prev.Start,
	NextID:           vCfg.Region.Next.Start,
	AffinityIndex:    vCfg.Region.Affinity.Start,
	AffinityBits:     vCfg.Region.Affinity.Bits,
	GossipIndex:      vCfg.Region.Gossip.Start,
	GossipBits:       vCfg.Region.Gossip.Bits,
	TTLIndex:         vCfg.Region.TTL.Start,
	TTLBits:          vCfg.Region.TTL.Bits,
	ProgramIndex:     vCfg.Region.Program.Start,
	ProgramBits:      vCfg.Region.Program.Bits,
	MaxPC:            int(vCfg.Region.Program.Bits) / 32,
	R0:               vCfg.Region.Registers.R0,
	R1:               vCfg.Region.Registers.R1,
	R2:               vCfg.Region.Registers.R2,
	R3:               vCfg.Region.Registers.R3,
	R4:               vCfg.Region.Registers.R4,
	R5:               vCfg.Region.Registers.R5,
	FW:               vCfg.Region.Registers.FW,
	RegPC:            vCfg.Region.Registers.PC,
}

/*
Config wraps viper with strict typed accessors that refuse to
return zero-values for missing keys.
*/
type Config struct {
	StateIndex       int
	StateSequence    int
	StateAccumulator int
	TokenIndex       int
	TokenBits        uint64
	ValueID          int
	PreviousID       int
	NextID           int
	AffinityIndex    int
	AffinityBits     uint64
	GossipIndex      int
	GossipBits       uint64
	TTLIndex         int
	TTLBits          uint64
	ProgramIndex     int
	ProgramBits      uint64
	MaxPC            int
	R0               int
	R1               int
	R2               int
	R3               int
	R4               int
	R5               int
	FW               int
	RegPC            int

	// Firmware holds compiled programs from config.yml, indexed by position.
	// Set a Value's firmware register to the index to select a program.
	Firmware      [][]uint32
	FirmwareIndex map[string]int
}

// CompileFunc is set by the primitive package to avoid circular imports.
// It compiles a program source string into instructions.
var CompileFunc func(string) ([]uint32, error)

// LoadFirmware compiles all programs from the config's `programs` section
// into Cfg.Firmware. Must be called after viper has loaded config.
func LoadFirmware() error {
	if CompileFunc == nil {
		return fmt.Errorf("core: CompileFunc not registered")
	}

	programs := viper.GetStringMapString("programs")
	if len(programs) == 0 {
		return nil
	}

	// Sort for deterministic indexing
	names := make([]string, 0, len(programs))
	for name := range programs {
		names = append(names, name)
	}
	sort.Strings(names)

	Cfg.Firmware = make([][]uint32, len(names))
	Cfg.FirmwareIndex = make(map[string]int, len(names))

	for i, name := range names {
		src := programs[name]
		instrs, err := CompileFunc(src)
		if err != nil {
			return fmt.Errorf("core: firmware %q: %w", name, err)
		}
		Cfg.Firmware[i] = instrs
		Cfg.FirmwareIndex[name] = i
	}

	return nil
}

func Get[T any](key string) T {
	return viper.GetViper().Get(key).(T)
}

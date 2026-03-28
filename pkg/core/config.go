package core

import (
	"fmt"
	"reflect"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/errnie"
)

type FirmwareType uint

const (
	FirmwareTypeNone FirmwareType = iota
	FirmwareTypeBootloader
	FirmwareTypeTombstone
	FirmwareTypeAffinity
	FirmwareTypeShatter
	FirmwareTypeWipe
	FirmwareTypeViral
)

/*
ValueConfig holds the configuration for a Value.
*/
type ValueConfig struct {
	Words   int                `mapstructure:"words"`
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
	Gossip    ValueOffsetConfig      `mapstructure:"gossip"`
	TTL       ValueOffsetConfig      `mapstructure:"ttl"`
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
vCfg holds the loaded Value configuration.
Populated by LoadValueConfig.
*/
var vCfg ValueConfig

/*
LoadValueConfig reads the "value" section from viper into V.
*/
func LoadValueConfig() (err error) {
	if err = viper.GetViper().UnmarshalKey(
		"value", &vCfg,
	); err != nil {
		return errnie.Error(
			NewConfigError(
				ConfigErrorTypeMissingKey,
				"value",
				err.Error(),
			),
		)
	}

	syncCfgFromVCfg()
	return LoadFirmware()
}

// syncCfgFromVCfg copies unmarshaled layout from vCfg into the global Cfg
// after LoadValueConfig unmarshals vCfg from config.
func syncCfgFromVCfg() {
	Cfg.StateIndex = vCfg.Region.State.Index
	Cfg.StateSequence = vCfg.Region.State.Sequence
	Cfg.StateAccumulator = vCfg.Region.State.Accumulator
	Cfg.TokenIndex = vCfg.Region.Tokens.Start
	Cfg.TokenBits = vCfg.Region.Tokens.Bits
	Cfg.ValueID = vCfg.Region.ID.Start
	Cfg.PreviousID = vCfg.Region.Prev.Start
	Cfg.NextID = vCfg.Region.Next.Start
	Cfg.AffinityIndex = vCfg.Region.Affinity.Start
	Cfg.AffinityBits = vCfg.Region.Affinity.Bits
	Cfg.GossipIndex = vCfg.Region.Gossip.Start
	Cfg.GossipBits = vCfg.Region.Gossip.Bits
	Cfg.TTLIndex = vCfg.Region.TTL.Start
	Cfg.TTLBits = vCfg.Region.TTL.Bits
	Cfg.ProgramIndex = vCfg.Region.Program.Start
	Cfg.ProgramBits = vCfg.Region.Program.Bits
	Cfg.MaxPC = int(vCfg.Region.Program.Bits) / 32
	Cfg.R0 = vCfg.Region.Registers.R0
	Cfg.R1 = vCfg.Region.Registers.R1
	Cfg.R2 = vCfg.Region.Registers.R2
	Cfg.R3 = vCfg.Region.Registers.R3
	Cfg.R4 = vCfg.Region.Registers.R4
	Cfg.R5 = vCfg.Region.Registers.R5
	Cfg.FW = vCfg.Region.Registers.FW
	Cfg.RegPC = vCfg.Region.Registers.PC
}

/*
Cfg is the global config accessor. Layout fields are populated by
LoadValueConfig; until then they are zero — call LoadValueConfig before use.
*/
var Cfg = new(Config)

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
	Firmware [FirmwareTypeViral + 1][]uint32
}

/*
LoadFirmware compiles all programs from the config's `programs` section
into Cfg.Firmware. Must be called after viper has loaded config.
*/
func compileAndAssign(ft FirmwareType, src string) error {
	program, err := CompileFunc(src)
	if err != nil {
		return errnie.Error(err)
	}
	Cfg.Firmware[ft] = program
	return nil
}

func LoadFirmware() error {
	fwBootloader := viper.GetViper().GetString("programs.bootloader")
	fwTombstone := viper.GetViper().GetString("programs.tombstone")
	fwAffinity := viper.GetViper().GetString("programs.affinity")
	fwShatter := viper.GetViper().GetString("programs.shatter")
	fwWipe := viper.GetViper().GetString("programs.wipe")
	fwViral := viper.GetViper().GetString("programs.viral")

	if err := compileAndAssign(FirmwareTypeBootloader, fwBootloader); err != nil {
		return err
	}
	if err := compileAndAssign(FirmwareTypeTombstone, fwTombstone); err != nil {
		return err
	}
	if err := compileAndAssign(FirmwareTypeAffinity, fwAffinity); err != nil {
		return err
	}
	if err := compileAndAssign(FirmwareTypeShatter, fwShatter); err != nil {
		return err
	}
	if err := compileAndAssign(FirmwareTypeWipe, fwWipe); err != nil {
		return err
	}
	if err := compileAndAssign(FirmwareTypeViral, fwViral); err != nil {
		return err
	}
	return nil
}

/*
Get is a type-safe wrapper around viper.GetViper().Get.
*/
func Get[T any](key string) (T, error) {
	var zero T
	raw := viper.GetViper().Get(key)
	if raw == nil {
		return zero, fmt.Errorf("config: missing key %q", key)
	}
	if v, ok := raw.(T); ok {
		return v, nil
	}
	return zero, fmt.Errorf(
		"config: key %q has type %T, want %s",
		key, raw, reflect.TypeOf(zero).String(),
	)
}

/*
ConfigErrorType is the type of a config error.
*/
type ConfigErrorType string

const (
	ConfigErrorTypeMissingKey   ConfigErrorType = "missing_key"
	ConfigErrorTypeInvalidValue ConfigErrorType = "invalid_value"
	ConfigErrorMissingFirmware  ConfigErrorType = "missing_firmware"
	ConfigErrorFirmwareCompile  ConfigErrorType = "firmware_compile"
)

/*
ConfigError is an error that occurs when loading the config.
*/
type ConfigError struct {
	Type ConfigErrorType
	Key  string
	Msg  string
}

/*
NewConfigError creates a new config error.
*/
func NewConfigError(t ConfigErrorType, key, msg string) ConfigError {
	return ConfigError{Type: t, Key: key, Msg: msg}
}

/*
Error returns the error message.
*/
func (e ConfigError) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Type, e.Key, e.Msg)
}

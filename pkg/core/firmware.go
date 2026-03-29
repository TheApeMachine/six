package core

// The firmware register is intentionally not a 1:1 mirror of the FirmwareType
// enum. Bootloader is installed directly into new Values and occupies the
// reserved bootstrap slots (PC 0..3); the in-band fw register only selects the
// self-programming payloads that should be loaded into the user program region
// (PC 4+).
const (
	FirmwareRegisterNone      uint64 = 0
	FirmwareRegisterLearn     uint64 = 1
	FirmwareRegisterTombstone uint64 = 2
	FirmwareRegisterViral     uint64 = 3
	FirmwareRegisterBuild     uint64 = 4

	// UserProgramPCStart is the first instruction slot after the persistent
	// bootstrap window. Compiled payload firmware is copied into slots 4+.
	UserProgramPCStart uint64 = 4
)

// FirmwareRegisterValue converts a host-side firmware type into the in-band
// fw register literal used by substrate programs. Bootloader is not addressable
// through fw because it is installed directly into the bootstrap slots.
func FirmwareRegisterValue(ft FirmwareType) uint64 {
	switch ft {
	case FirmwareTypeLearn:
		return FirmwareRegisterLearn
	case FirmwareTypeTombstone:
		return FirmwareRegisterTombstone
	case FirmwareTypeViral:
		return FirmwareRegisterViral
	case FirmwareTypeBuild:
		return FirmwareRegisterBuild
	default:
		return FirmwareRegisterNone
	}
}

// ResolveFirmwareRegister maps the in-band fw register literal back to the
// compiled firmware payload that should be copied into the user program region.
// Bootloader is intentionally excluded.
func ResolveFirmwareRegister(reg uint64) (FirmwareType, bool) {
	switch reg {
	case FirmwareRegisterLearn:
		return FirmwareTypeLearn, true
	case FirmwareRegisterTombstone:
		return FirmwareTypeTombstone, true
	case FirmwareRegisterViral:
		return FirmwareTypeViral, true
	case FirmwareRegisterBuild:
		return FirmwareTypeBuild, true
	default:
		return 0, false
	}
}

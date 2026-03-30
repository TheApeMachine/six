package core

const (
	// Firmware register protocol values carried in-band by Value frames.
	// These are distinct from the FirmwareType enum because Bootloader is not
	// selected through the fw register; new frames already begin with the
	// bootloader installed in the system slots.
	FirmwareRegisterNone uint64 = iota
	FirmwareRegisterLearn
	FirmwareRegisterTombstone
	FirmwareRegisterViral
	FirmwareRegisterBuild
)

const (
	// PayloadProgramWordOffset is the fixed payload span used by the in-band
	// programs themselves (config.yml refers to bits 5120..8192 => words 80..127).
	// The first four program words remain the persistent bootstrap prefix. The
	// bootloader may occupy more slots during cold boot, but sequenced payloads
	// intentionally overwrite the remainder in place after bootstrapping.
	PayloadProgramWordOffset = 4
	PayloadProgramPCOffset   = PayloadProgramWordOffset * 2

	// UserProgramPCStart is kept as a compatibility alias for older call sites.
	UserProgramPCStart uint64 = PayloadProgramPCOffset
)

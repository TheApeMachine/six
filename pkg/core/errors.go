package core

import "fmt"

/*
ConfigErrorType is the type of a config error.
*/
type ConfigErrorType string

const (
	ConfigErrorMissingKey      ConfigErrorType = "missing_key"
	ConfigErrorInvalidValue    ConfigErrorType = "invalid_value"
	ConfigErrorMissingFirmware ConfigErrorType = "missing_firmware"
	ConfigErrorFirmwareCompile ConfigErrorType = "firmware_compile"
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

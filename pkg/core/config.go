package core

import (
	"github.com/spf13/viper"
)

/*
Cfg is the global config accessor. All reads go through typed methods
that error when the requested key is absent, so callers never silently
operate on zero-values from a missing key.
*/
var Cfg = &Config{}

/*
Config wraps viper with strict typed accessors that refuse to
return zero-values for missing keys.
*/
type Config struct{}

func Get[T any](key string) T {
	return viper.GetViper().Get(key).(T)
}

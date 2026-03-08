package config

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/console"
	"k8s.io/client-go/util/homedir"
)

/*
Canonical architecture constants for type definitions.
Go requires compile-time array sizes; runtime values live in Architecture.
These must match config defaults.
*/
const (
	NBasis      = 512
	ChordBlocks = NBasis / 64
)

var ctx = &Config{}
var Numeric = &ctx.Architecture
var System = &ctx.System
var Workers = &ctx.Workers

func init() {
	viper.SetDefault("system.workers.min", 2)
	viper.SetDefault("system.workers.max", "CPU")
	viper.SetDefault("system.distributed.workers", []string{"localhost:8080", "localhost:8081"})
	viper.SetDefault("system.distributed.chunk", 2048)
	viper.SetDefault("system.distributed.timeout", 2000)
	viper.SetDefault("system.distributed.remoteOnly", false)
	viper.SetDefault("system.distributed.heteroLocal", false)
	viper.SetDefault("system.distributed.localShardThreshold", 4096)

	viper.SetDefault("architecture.numerics.epsilon", 1e-9)
	viper.SetDefault("architecture.numerics.nsymbols", 256)
	viper.SetDefault("architecture.numerics.nbasis", 512)
	viper.SetDefault("architecture.numerics.chordBlocks", 16)
	viper.SetDefault("architecture.numerics.frequencySpread", 8)

	home := homedir.HomeDir()

	viper.SetConfigFile(filepath.Join(home, ".six", "config.yml"))
	_ = viper.ReadInConfig()

	_, err := New()

	if err != nil {
		os.Exit(1)
	}
}

/*
Config holds the singleton configuration for the runtime.
Binds architecture numerics, distributed system params, and worker limits.
*/
type Config struct {
	Architecture Architecture
	System       Distributed
	Workers      struct {
		Min int
		Max int
	}
}

/*
Architecture holds numerics for chord dimension, basis size, and frequency spread.
Drives compile-time array allocation and runtime computations.
*/
type Architecture struct {
	Epsilon         float64
	NSymbols        int
	NBasis          int
	Windows         []int
	WindowWeights   []float64
	ChordBlocks     int
	FrequencySpread float64
}

/*
Distributed holds worker endpoints, chunk size, and sharding behavior.
Controls whether work runs local, remote, or hybrid.
*/
type Distributed struct {
	Workers             []string
	Chunk               int
	Timeout             int
	RemoteOnly          bool
	HeteroLocal         bool
	LocalShardThreshold int
}

var loadOnce sync.Once
var loadErr error

/*
New returns the singleton Config, loading from viper on first call.
Thread-safe via sync.Once.
*/
func New() (*Config, error) {
	loadOnce.Do(func() {
		loadErr = ctx.Load()
	})
	return ctx, loadErr
}

/*
Load populates Config from viper, validating NBasis and worker limits.
Exits with non-zero on mismatch or invalid config.
*/
func (ctx *Config) Load() error {
	v := viper.GetViper()

	ctx.Architecture.Epsilon = v.GetFloat64("architecture.numerics.epsilon")
	ctx.Architecture.NSymbols = v.GetInt("architecture.numerics.nsymbols")
	ctx.Architecture.NBasis = v.GetInt("architecture.numerics.nbasis")

	if ctx.Architecture.NBasis != NBasis {
		return console.Error(
			ConfigError("architecture.numerics.nbasis mismatch"),
			"expected",
			NBasis,
			"got",
			ctx.Architecture.NBasis,
		)
	}
	ctx.Architecture.Windows = v.GetIntSlice("architecture.numerics.windows")
	ctx.Architecture.ChordBlocks = ctx.Architecture.NBasis / 64
	ctx.Architecture.FrequencySpread = math.Log2(float64(ctx.Architecture.NBasis))

	minWorkers := v.GetInt("system.workers.min")
	maxWorkersStr := v.GetString("system.workers.max")
	maxWorkers := v.GetInt("system.workers.max")

	if maxWorkersStr == "CPU" {
		maxWorkers = runtime.NumCPU()
	}

	ctx.Workers.Min = minWorkers
	ctx.Workers.Max = maxWorkers

	if maxWorkers == 0 || maxWorkers < minWorkers {
		return console.Error(
			ErrBadMaxWorkerConfig,
			"max workers",
			maxWorkersStr,
			"min workers",
			minWorkers,
		)
	}

	ctx.System.Workers = v.GetStringSlice("system.distributed.workers")
	ctx.System.Chunk = v.GetInt("system.distributed.chunk")
	ctx.System.Timeout = v.GetInt("system.distributed.timeout")
	ctx.System.RemoteOnly = v.GetBool("system.distributed.remoteOnly")
	ctx.System.HeteroLocal = v.GetBool("system.distributed.heteroLocal")
	ctx.System.LocalShardThreshold = v.GetInt("system.distributed.localShardThreshold")

	return nil
}

/*
ConfigError is a typed error for config validation failures.
Enables typed checks in console output.
*/
type ConfigError string

const (
	ErrBadMaxWorkerConfig ConfigError = "max workers config is bad"
)

/*
Error implements the error interface for ConfigError.
*/
func (err ConfigError) Error() string {
	return string(err)
}

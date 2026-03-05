package config

import (
	"os"
	"runtime"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/console"
)

var ctx = &Config{}

func init() {
	viper.SetDefault("system.workers.min", 2)
	viper.SetDefault("system.workers.max", "CPU")
	viper.SetDefault("system.distributed.workers", []string{"localhost:8080", "localhost:8081"})
	viper.SetDefault("system.distributed.chunk", 2048)
	viper.SetDefault("system.distributed.timeout", 2000)
	viper.SetDefault("system.distributed.remoteOnly", false)
	viper.SetConfigFile("/Users/theapemachine/go/src/github.com/theapemachine/six/cmd/cfg/config.yml")
	_ = viper.ReadInConfig()

	ctx = New()

	if err := ctx.Load(); err != nil {
		os.Exit(1)
	}
}

type Config struct {
	System System
	Architecture Architecture
}

type System struct {
	Workers Workers
	Distributed Distributed
}

type Workers struct {
	Min int
	Max int
}

type Distributed struct {
	Workers []string
	Chunk int
	Timeout int
	RemoteOnly bool
}

type Architecture struct {
	Numerics Numerics
}

type Numerics struct {
	Epsilon float64
	NSymbols int
	NBasis int
	Windows []int
}

func New() *Config {
	return &Config{}
}

func Get() *Config {
	return ctx
}

func (c *Config) Load() error {
	v := viper.GetViper()

	c.System.Workers.Min = v.GetInt("system.workers.min")
	c.System.Workers.Max = v.GetInt("system.workers.max")

	minWorkers := v.GetInt("system.workers.min")

	maxWorkersStr := v.GetString("system.workers.max")
	maxWorkers := v.GetInt("system.workers.max")
	
	if maxWorkersStr == "CPU" {
		maxWorkers = runtime.NumCPU()
	}

	if maxWorkers == 0 || maxWorkers < minWorkers {
		return console.Error(
			ErrBadMaxWorkerConfig, 
			"max workers", 
			maxWorkersStr, 
			"min workers", 
			minWorkers,
		)
	}

	c.System.Distributed.Workers = v.GetStringSlice("system.distributed.workers")
	c.System.Distributed.Chunk = v.GetInt("system.distributed.chunk")
	c.System.Distributed.Timeout = v.GetInt("system.distributed.timeout")
	c.System.Distributed.RemoteOnly = v.GetBool("system.distributed.remoteOnly")

	c.Architecture.Numerics.Epsilon = v.GetFloat64("architecture.numerics.epsilon")
	c.Architecture.Numerics.NSymbols = v.GetInt("architecture.numerics.nsymbols")
	c.Architecture.Numerics.NBasis = v.GetInt("architecture.numerics.nbasis")
	c.Architecture.Numerics.Windows = v.GetIntSlice("architecture.numerics.windows")

	return nil
}


type ConfigError string

const (
	ErrBadMaxWorkerConfig ConfigError = "max workers config is bad"
)

func (err ConfigError) Error() string {
	return string(err)
}
package pool

import "time"

// Config holds pool-wide settings.
type Config struct {
	SchedulingTimeout time.Duration
}

// NewConfig returns a Config with sensible defaults.
func NewConfig() *Config {
	return &Config{
		SchedulingTimeout: 10 * time.Second,
	}
}

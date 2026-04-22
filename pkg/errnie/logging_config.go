package errnie

// LoggingConfig is loaded from the config file key "logging" (see cmd/cfg/config.yml).
// Unmarshaled by errnie.InitLoggerFromViper after viper.ReadInConfig.
type LoggingConfig struct {
	Logfile       bool                `mapstructure:"logfile"`
	Trace         TraceLoggingConfig  `mapstructure:"trace"`
	Elasticsearch ElasticsearchConfig `mapstructure:"elasticsearch"`
}

// TraceLoggingConfig controls the optional plain-text trace file.
type TraceLoggingConfig struct {
	// Path is the trace log file. Empty means ./trace.log under the process working directory.
	Path string `mapstructure:"path"`
}

// ElasticsearchConfig controls structured log shipping to Elasticsearch.
type ElasticsearchConfig struct {
	Enabled            bool     `mapstructure:"enabled"`
	URLs               []string `mapstructure:"urls"`
	Index              string   `mapstructure:"index"`
	Username           string   `mapstructure:"username"`
	Password           string   `mapstructure:"password"`
	APIKey             string   `mapstructure:"api_key"`
	CACertPath         string   `mapstructure:"ca_cert"`
	InsecureSkipVerify bool     `mapstructure:"insecure_skip_verify"`

	// BulkFlushBytes caps bulk batch size; 1 means flush after each document (low latency).
	BulkFlushBytes int `mapstructure:"bulk_flush_bytes"`
	// FlushIntervalMS is the max time before an auto-flush; lower = fresher logs, more requests.
	FlushIntervalMS int `mapstructure:"flush_interval_ms"`
	// BulkRefresh is passed to the bulk API (e.g. "true", "wait_for", "false"). Empty defaults to "false" (no forced refresh per bulk).
	BulkRefresh string `mapstructure:"bulk_refresh"`
}


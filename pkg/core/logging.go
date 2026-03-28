package core

import (
	"os"
	"strings"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/errnie"
)

// LoadLoggingConfig reads the "logging" subtree from Viper into errnie.LoggingConfig.
// Call after the config file (or embedded default) has been loaded.
//
// When password or api_key are empty in YAML, ELASTICSEARCH_PASSWORD / ELASTIC_PASSWORD
// and ELASTICSEARCH_API_KEY are used so secrets can stay out of the repo.
func LoadLoggingConfig() (errnie.LoggingConfig, error) {
	var c errnie.LoggingConfig
	if err := viper.GetViper().UnmarshalKey("logging", &c); err != nil {
		return c, err
	}
	if c.Elasticsearch.Password == "" {
		if p := os.Getenv("ELASTICSEARCH_PASSWORD"); p != "" {
			c.Elasticsearch.Password = p
		} else if p := os.Getenv("ELASTIC_PASSWORD"); p != "" {
			c.Elasticsearch.Password = p
		}
	}
	if c.Elasticsearch.APIKey == "" {
		c.Elasticsearch.APIKey = os.Getenv("ELASTICSEARCH_API_KEY")
	}
	// Optional toggle for Compose/CI when config.yml is not updated.
	if envTruthy("ELASTICSEARCH_ENABLED") {
		c.Elasticsearch.Enabled = true
	}
	return c, nil
}

func envTruthy(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

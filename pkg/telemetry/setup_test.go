package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
)

func resolveTelemetryTestConfigPath() string {
	if e := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); e != "" {
		return filepath.Clean(e)
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"))
	}
	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveTelemetryTestConfigPath())
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "telemetry tests: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "telemetry tests: core.LoadValueConfig: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

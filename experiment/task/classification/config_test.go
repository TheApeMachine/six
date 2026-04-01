package classification

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

func resolveClassificationTestConfigPath() string {
	if env := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); env != "" {
		return filepath.Clean(env)
	}

	_, file, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "cmd", "cfg", "config.yml"))
	}

	return filepath.Clean(filepath.Join("..", "..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveClassificationTestConfigPath())

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "classification tests: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}

	viper.Set("loglevel", "error")
	viper.Set("logging.trace.path", os.DevNull)
	core.NewConfig()

	loggingCfg, err := core.LoadLoggingConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "classification tests: core.LoadLoggingConfig: %v\n", err)
		os.Exit(1)
	}

	errnie.InitLogger(loggingCfg)

	code := m.Run()
	_ = errnie.Shutdown(context.Background())
	os.Exit(code)
}

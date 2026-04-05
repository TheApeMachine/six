package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
)

func TestMain(m *testing.M) {
	viper.SetConfigType("yml")
	configPath := filepath.Join("..", "..", "cmd", "cfg", "config.yml")
	viper.SetConfigFile(configPath)

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "vm tests: viper.ReadInConfig(%s): %v\n", configPath, err)
		os.Exit(1)
	}

	core.NewConfig()
	os.Exit(m.Run())
}

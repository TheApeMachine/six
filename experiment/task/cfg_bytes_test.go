//go:build ignore

package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
)

func main() {
	viper.SetConfigType("yml")
	viper.SetConfigFile(filepath.Join("cmd", "cfg", "config.yml"))
	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}
	core.NewConfig()
	fmt.Println("Value.Bytes", core.Cfg.Value.Bytes)
}

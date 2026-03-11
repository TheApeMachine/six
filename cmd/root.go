package cmd

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/errnie"
	"github.com/theapemachine/six/utils"
)

/*
Embed a mini filesystem into the binary to hold the default config file.
This will be written to the home directory of the user running the service,
which allows a developer to easily override the config file.
*/
//go:embed cfg/*
var embedded embed.FS

var (
	projectName = "six"
	cfgFile     string
	Alice       []byte

	rootCmd = &cobra.Command{
		Use:   "six",
		Short: "Check yo six",
		Long:  roottxt,
		Run: func(cmd *cobra.Command, args []string) {
		},
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(
		&cfgFile, "config", "config.yml", "config file (default is $HOME/."+projectName+"/config.yml)",
	)
}

func initConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath("$HOME/." + projectName)

	if err := viper.ReadInConfig(); err != nil {
		// Just log loosely since we now optionally rely on embedded config/defaults
		console.Warn(
			"Note: No local config file found, using defaults.",
			"error", err,
		)
	}

	Alice = errnie.FlatMap(
		errnie.Try(embedded.Open("cfg/alice.txt")),
		func(fh fs.File) ([]byte, error) {
			defer fh.Close()
			var buf bytes.Buffer

			if _, err := io.Copy(&buf, fh); err != nil {
				return nil, err
			}

			return buf.Bytes(), nil
		},
	).Value()
}

func writeConfig() error {
	home, _ := os.UserHomeDir()
	fullPath := home + "/." + projectName + "/" + cfgFile

	if utils.CheckFileExists(fullPath) {
		return nil
	}

	result := errnie.FlatMap(
		errnie.Try(embedded.Open("cfg/"+cfgFile)),
		func(fh fs.File) ([]byte, error) {
			defer fh.Close()
			var buf bytes.Buffer

			if _, err := io.Copy(&buf, fh); err != nil {
				return nil, err
			}

			return buf.Bytes(), nil
		},
	)

	result = errnie.FlatMap(result, func(data []byte) ([]byte, error) {
		if err := os.MkdirAll(home+"/."+projectName, os.ModePerm); err != nil {
			return nil, err
		}
		return data, nil
	})

	result = errnie.FlatMap(result, func(data []byte) ([]byte, error) {
		return data, os.WriteFile(fullPath, data, 0644)
	})

	return result.Err()
}

const roottxt = `
six v0.0.1
`

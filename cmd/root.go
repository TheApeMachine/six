package cmd

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/core/utils"
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

	/*
		Alice holds the default dataset/context used by the visualizer and tests.
		It is loaded from embedded filesystem and available globally after initConfig.
	*/
	Alice []byte

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
		&cfgFile,
		"config",
		"config.yml",
		"config file (default is $HOME/."+projectName+"/config.yml)",
	)
}

func initConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath("$HOME/." + projectName)

	if err := viper.ReadInConfig(); err != nil {
		console.Warn(ErrConfigNotFound, "error", err)
	}

	result := errnie.Then(
		errnie.Try(embedded.Open("cfg/alice.txt")),
		func(fh fs.File) ([]byte, error) {
			defer fh.Close()
			var buf bytes.Buffer

			if _, err := io.Copy(&buf, fh); err != nil {
				return nil, err
			}

			return buf.Bytes(), nil
		},
	)

	if err := result.Err(); err != nil {
		console.Error(err, "msg", "Failed to load embedded alice.txt")
		os.Exit(1)
	} else {
		Alice = result.Value()
	}
}

func writeConfig() error {
	home, _ := os.UserHomeDir()
	fullPath := home + "/." + projectName + "/" + cfgFile

	if utils.CheckFileExists(fullPath) {
		return nil
	}

	result := errnie.Then(
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

	result = errnie.Then(result, func(data []byte) ([]byte, error) {
		if err := os.MkdirAll(home+"/."+projectName, os.ModePerm); err != nil {
			return nil, err
		}
		return data, nil
	})

	result = errnie.Then(result, func(data []byte) ([]byte, error) {
		return data, os.WriteFile(fullPath, data, 0644)
	})

	return result.Err()
}

type RootError string

func (err RootError) Error() string {
	return string(err)
}

func (err RootError) String() string {
	return string(err)
}

const (
	ErrConfigNotFound RootError = "no local config file found, using defaults"
)

const roottxt = `
six v0.0.1
`

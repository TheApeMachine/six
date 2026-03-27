package cmd

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
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

/*
Execute executes the root command.
*/
func Execute() error {
	return rootCmd.Execute()
}

/*
init configures cobra and registers the config flag.
*/
func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(
		&cfgFile,
		"config",
		"config.yml",
		"config file (default is $HOME/."+projectName+"/config.yml)",
	)
}

/*
initConfig reads in config file and ENV variables if set.
Tries the local filesystem first ($HOME/.six/config.yml), then
falls back to the binary-embedded default so the process always
starts with a valid configuration.
*/
func initConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath("$HOME/." + projectName)

	if err := viper.ReadInConfig(); err != nil {
		errnie.Warn(NewRootError(RootErrorTypeConfigNotFound).Error())

		cfgReader, openErr := embedded.Open("cfg/config.yml")

		if openErr != nil {
			errnie.Error(NewRootError(RootErrorTypeEmbeddedConfigFailed))
			return
		}

		defer cfgReader.Close()

		if readErr := viper.ReadConfig(cfgReader); readErr != nil {
			errnie.Error(NewRootError(RootErrorTypeEmbeddedConfigFailed))
		}
	}

	// Always ensure the core value config is populated from whichever source we loaded
	if err := core.LoadValueConfig(); err != nil {
		errnie.Error(fmt.Errorf("failed to load value config: %w", err))
	}

	backend, err := compute.NewBackend(
		compute.WithContext(context.Background()),
	)
	if err != nil {
		errnie.Error(fmt.Errorf("compute.NewBackend: %w", err))
		os.Exit(1)
	}
	primitive.Backend = backend
}

const roottxt = `
six v0.0.1
`

type RootErrorType string

const (
	RootErrorTypeConfigNotFound       RootErrorType = "no local config file found, using defaults"
	RootErrorTypeEmbeddedConfigFailed RootErrorType = "failed to read embedded config"
)

/*
RootError represents errors related to the
root command setup and configuration.
*/
type RootError struct {
	Message string
	Err     error
}

/*
NewRootError creates a new RootError.
*/
func NewRootError(err RootErrorType) *RootError {
	return &RootError{Message: string(err), Err: errors.New(string(err))}
}

/*
Error returns the string representation of the RootError.
*/
func (err *RootError) Error() string {
	return fmt.Errorf(
		"[root] %s: %w", err.Message, err.Err,
	).Error()
}

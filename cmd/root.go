package cmd

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/telemetry"
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
	Backend     *compute.Backend

	// initErr records fatal errors from initConfig (e.g. LoadLoggingConfig,
	// compute.NewBackend) so Execute can return them instead of os.Exit.
	initErr error

	/*
		Alice holds the default dataset/context used by the visualizer and tests.
		It is loaded from embedded filesystem and available globally after initConfig.
	*/
	Alice []byte

	rootCmd = &cobra.Command{
		Use:   "six",
		Short: "Check yo six",
		Long:  roottxt,
		// One structured log per invocation so shipping sinks (e.g. Elasticsearch) always see activity.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if initErr != nil {
				return initErr
			}
			errnie.Info(
				"six.run",
				"command", cmd.CommandPath(),
				"args", strings.Join(args, " "),
			)
			return nil
		},
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
		"",
		"path to config file (default: try cmd/cfg/config.yml, ./config.yml, $HOME/."+projectName+"/config.yml, then embedded default)",
	)
}

/*
initConfig loads config.yml from, in order:
  - path given by --config (if set)
  - ./cmd/cfg/config.yml (repo checkout)
  - ./config.yml
  - $HOME/.six/config.yml
  - embedded cmd/cfg/config.yml
*/
func initConfig() {
	viper.SetConfigType("yml")

	tryRead := func(path string) error {
		viper.SetConfigFile(path)
		return viper.ReadInConfig()
	}

	loaded := false
	if rootCmd.PersistentFlags().Changed("config") && strings.TrimSpace(cfgFile) != "" {
		if err := tryRead(cfgFile); err == nil {
			loaded = true
		} else {
			errnie.Warn(fmt.Sprintf("config file %q unreadable: %v", cfgFile, err))
		}
	}

	if !loaded {
		paths := []string{
			"cmd/cfg/config.yml",
			"config.yml",
		}
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, "."+projectName, "config.yml"))
		}
		for _, p := range paths {
			if err := tryRead(p); err == nil {
				loaded = true
				break
			}
		}
	}

	if !loaded {
		errnie.Warn(NewRootError(RootErrorTypeConfigNotFound).Error())
		cfgReader, openErr := embedded.Open("cfg/config.yml")
		if openErr != nil {
			errnie.Error(NewRootError(RootErrorTypeEmbeddedConfigFailed))
			return
		}
		defer cfgReader.Close()
		if readErr := viper.ReadConfig(cfgReader); readErr != nil {
			errnie.Error(NewRootError(RootErrorTypeEmbeddedConfigFailed))
			return
		}
	}

	/*
		core.init runs NewConfig while viper is still empty, so Value.Bytes and the
		rest of Cfg are zero until we rebuild from the loaded file. Without this,
		the tokenizer and vm.Machine IO loop see value.bytes==0 and return
		io.ErrShortBuffer forever.
	*/
	core.NewConfig()
	telemetry.ConfigureFromConfig()

	if err := errnie.InitLoggerFromViper(); err != nil {
		initErr = fmt.Errorf("%w", err)
	}
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

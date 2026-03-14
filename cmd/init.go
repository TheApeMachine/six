package cmd

import (
	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/system/console"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize default configuration",
	Long:  initLong,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := writeConfig(); err != nil {
			return console.Error(ErrConfigInitFailed, "err", err)
		}

		console.Info("configuration initialized")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

type InitError string

func (e InitError) Error() string {
	return string(e)
}

const (
	ErrConfigInitFailed InitError = "failed to initialize configuration"
)

const initLong = `
Initialize the default configuration file ($HOME/.six/config.yml).

This command will create the default configuration file in the user's home directory.
If the configuration file already exists, it will be overwritten.

Examples:
	six init
`

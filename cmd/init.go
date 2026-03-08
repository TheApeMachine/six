package cmd

import (
	"github.com/spf13/cobra"
	"github.com/theapemachine/six/console"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize default configuration",
	Long:  `Explicitly generate the default local configuration file ($HOME/.six/config.yml).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := writeConfig(); err != nil {
			return console.Error(err, "stage", "config initialization")
		}

		console.Info("Configuration successfully populated.")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

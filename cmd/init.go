package cmd

import (
	"log"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize default configuration",
	Long:  `Explicitly generate the default local configuration file ($HOME/.six/config.yml).`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := writeConfig(); err != nil {
			log.Fatalf("Error initializing configuration: %v\n", err)
		} else {
			log.Println("Configuration successfully populated.")
		}
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

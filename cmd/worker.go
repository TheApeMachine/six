package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var workerAddr string

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run distributed best-fill worker",
	Long:  "Starts a distributed worker. Not yet ported to the new Backend interface.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("distributed workers not yet ported to the new Backend interface")
	},
}

func init() {
	workerCmd.Flags().StringVar(&workerAddr, "addr", ":7777", "worker listen address")
	rootCmd.AddCommand(workerCmd)
}

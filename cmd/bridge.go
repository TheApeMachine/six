package cmd

import (
	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/telemetry"
)

var bridgeCmd = &cobra.Command{
	Use:   "bridge",
	Short: "Run the bridge",
	Long:  "Run the bridge",
	RunE: func(cmd *cobra.Command, args []string) error {
		bridge, err := telemetry.NewBridge(cmd.Context(), "ws://localhost:6600")

		if err != nil {
			return err
		}

		if err := bridge.ListenAndServe(); err != nil {
			errnie.Error(err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(bridgeCmd)
}

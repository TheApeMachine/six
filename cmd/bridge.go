package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/errnie"
)

var bridgeCmd = &cobra.Command{
	Use:   "bridge",
	Short: "Run the visualizer WebSocket telemetry hub (Node)",
	Long: `Runs the fan-out hub that accepts WebSocket clients on port 6600
(see visualizer/server/bridge.ts). The Go runtime connects to this hub as a
client (via telemetry.ws_url in config, often through the Vite /ws proxy).

From the repository root: same as "cd visualizer && npm run bridge".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		workDir, err := os.Getwd()
		if err != nil {
			return errnie.Error(err)
		}

		vizDir := filepath.Join(workDir, "visualizer")
		if _, statErr := os.Stat(filepath.Join(vizDir, "package.json")); statErr != nil {
			return errnie.Error(fmt.Errorf(
				"visualizer/package.json not found under %s — run this command from the repo root",
				workDir,
			))
		}

		c := exec.CommandContext(cmd.Context(), "npm", "run", "bridge")
		c.Dir = vizDir
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin

		if runErr := c.Run(); runErr != nil {
			return errnie.Error(runErr)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(bridgeCmd)
}


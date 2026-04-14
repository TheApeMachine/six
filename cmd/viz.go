package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/viz"
)

var (
	vizAddr string
	vizDemo bool
)

var vizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Start the 3D system visualization server",
	Long:  vizLong,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM,
		)

		defer cancel()

		server := viz.NewServer(viz.DefaultBus, vizAddr)

		viz.SetProgramsProvider(func() map[string]string {
			if core.Cfg == nil {
				return map[string]string{}
			}

			return core.Cfg.Programs
		})

		errnie.Info("viz.start", "addr", vizAddr)

		return server.Start(ctx)
	},
}

func init() {
	vizCmd.Flags().StringVar(
		&vizAddr,
		"addr",
		":6600",
		"address to bind the visualization server",
	)

	vizCmd.Flags().BoolVar(
		&vizDemo,
		"demo",
		false,
		"run a live demo mesh that generates events for the visualizer",
	)

	rootCmd.AddCommand(vizCmd)
}

var vizLong = `
Launches a web-based 3D visualization of the Six system showing nodes,
connections, data flow, field dynamics, and adaptive state in real time.

Use --demo to run a self-contained mesh that generates events.
`

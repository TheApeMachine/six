package cmd

import (
	"github.com/spf13/cobra"
)

var vizAddr string

var vizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Start the 3D system visualization server",
	Long:  vizLong,
	RunE: func(cmd *cobra.Command, args []string) error {
		// ctx, cancel := signal.NotifyContext(
		// 	context.Background(), os.Interrupt, syscall.SIGTERM,
		// )

		// defer cancel()

		// // Wire compute pool telemetry into the viz bus.
		// compute.SetPoolEmitFunc(func(ev compute.PoolEvent) {
		// 	if ev.Action == "complete" || ev.Action == "done" {
		// 		viz.DefaultBus.Publish(
		// 			viz.PoolCompleteEvent(
		// 				ev.Action, ev.DurationMs,
		// 			),
		// 		)
		// 	} else {
		// 		viz.DefaultBus.Publish(
		// 			viz.PoolScheduleEvent(
		// 				ev.Action,
		// 				ev.QueueSize,
		// 				ev.Workers,
		// 			),
		// 		)
		// 	}
		// })

		// server := viz.NewServer(viz.DefaultBus, vizAddr)

		// errnie.Info("viz.start", "addr", vizAddr)

		// return server.Start(ctx)

		return nil
	},
}

func init() {
	vizCmd.Flags().StringVar(
		&vizAddr,
		"addr",
		":6600",
		"address to bind the visualization server",
	)

	rootCmd.AddCommand(vizCmd)
}

var vizLong = `
Launches a web-based 3D visualization of the Six system showing nodes, 
connections, data flow, field dynamics, and adaptive state in real time.
`

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/provider/local"
	"github.com/theapemachine/six/visualizer"
)

var vizListen bool

var vizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Launch the 3D chord geometry visualizer",
	Long: `Starts a WebSocket server and opens a Three.js visualization of chord operations in real-time.

By default runs the Alice demo. Use --listen to start in listener mode,
which receives real telemetry from the running system via UDP.`,
	Run: func(cmd *cobra.Command, args []string) {
		server := visualizer.NewServer()

		mode := "demo"
		if vizListen {
			mode = "listener (waiting for real system telemetry on UDP :8258)"
		}

		fmt.Printf("Visualizer running at http://localhost:8257 [%s]\n", mode)
		fmt.Println("Open in browser to see the 3D chord space")

		go func() {
			if err := server.ListenAndServe(":8257"); err != nil && cmd.Context().Err() == nil {
				console.Error(err, "msg", "Server error")
				os.Exit(1)
			}
		}()

		if !vizListen {
			dataset := local.New(local.WithBytes(Alice))
			if err := visualizer.RunAliceDemo(
				cmd.Context(),
				dataset,
			); err != nil && cmd.Context().Err() == nil {
				console.Error(err, "msg", "Demo error")
			}
		} else {
			<-cmd.Context().Done()
		}
	},
}

func init() {
	vizCmd.Flags().BoolVarP(&vizListen, "listen", "l", false, "Listen-only mode: no demo, just receive real system telemetry")
	rootCmd.AddCommand(vizCmd)
}

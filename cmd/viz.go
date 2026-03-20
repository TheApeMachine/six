package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/system/vm"
	"github.com/theapemachine/six/visualizer"
)

var vizListen bool

var vizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Launch the 3D value geometry visualizer",
	Long: `Starts a WebSocket server and opens a Three.js visualization of value operations in real-time.

By default runs the Alice demo. Use --listen to start in listener mode,
which receives real telemetry from the running system via UDP.`,
	Run: func(cmd *cobra.Command, args []string) {
		server := visualizer.NewServer()
		workerPool := pool.New(
			cmd.Context(),
			1,
			runtime.NumCPU(),
			&pool.Config{},
		)
		defer workerPool.Close()

		mode := "demo"
		if vizListen {
			mode = "listener (waiting for real system telemetry on UDP :8258)"
		}

		fmt.Printf("Visualizer running at http://localhost:8257 [%s]\n", mode)
		fmt.Println("Open in browser to see the 3D value space")

		go func() {
			if err := server.ListenAndServe(":8257"); err != nil && cmd.Context().Err() == nil {
				console.Error(err, "msg", "Server error")
				os.Exit(1)
			}
		}()

		if vizListen {
			machine := vm.NewMachine(vm.MachineWithContext(cmd.Context()))
			defer machine.Close()

			server.SetPromptFunc(machine.Prompt)
			server.SetIngestFunc(func(raw []byte) error {
				return machine.SetDataset(local.New(local.WithBytes(raw)))
			})
		} else {
			dataset := local.New(local.WithBytes(Alice))

			if err := visualizer.RunAliceDemo(
				cmd.Context(),
				dataset,
				server,
			); err != nil && cmd.Context().Err() == nil {
				console.Error(err, "msg", "Demo error")
				return
			}
		}

		<-cmd.Context().Done()
	},
}

func init() {
	vizCmd.Flags().BoolVarP(&vizListen, "listen", "l", false, "Listen-only mode: no demo, just receive real system telemetry")
	rootCmd.AddCommand(vizCmd)
}

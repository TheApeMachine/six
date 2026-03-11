package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/visualizer"
)

var vizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Launch the 3D chord geometry visualizer",
	Long:  "Starts a WebSocket server and opens a Three.js visualization of chord operations in real-time.",
	Run: func(cmd *cobra.Command, args []string) {
		server := visualizer.NewServer()

		go func() {
			if err := visualizer.RunAliceDemo(server, "cmd/cfg/alice.txt"); err != nil {
				fmt.Println("Demo error:", err)
			}
		}()

		fmt.Println("Visualizer running at http://localhost:8257")
		fmt.Println("Open in browser to see the 3D chord space")

		if err := server.ListenAndServe(":8257"); err != nil {
			fmt.Println("Server error:", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(vizCmd)
}

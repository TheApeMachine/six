package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/visualizer"
)

var (
	vizHTTPAddr string
	vizUDPAddr  string
	vizRepo     string
	vizSubset   string
	vizColumn   string
	vizIters    int
	vizDelay    time.Duration
	vizListen   bool
)

var vizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Run the 3D substrate visualizer with live telemetry",
	Long: `Starts the HTTP/WebSocket visualizer and optionally runs the experiment
substrate loop. Use --listen to skip the dataset loop and only serve the UI,
accepting graph events via UDP from external test runs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv := visualizer.NewServer()

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		if !vizListen {
			go func() {
				err := visualizer.RunSubstrateLoop(ctx, srv, visualizer.SubstrateOpts{
					Repo:       vizRepo,
					Subset:     vizSubset,
					TextColumn: vizColumn,
					Iterations: vizIters,
					StepDelay:  vizDelay,
				})

				if err != nil && ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "substrate loop: %v\n", err)
				}
			}()
		}

		go func() {
			<-ctx.Done()
			srv.Shutdown()
		}()

		udpHint := "auto"
		if vizUDPAddr != "" {
			udpHint = vizUDPAddr
		}

		mode := "substrate"
		if vizListen {
			mode = "listen-only (send events via UDP)"
		}

		fmt.Fprintf(os.Stderr, "visualizer http://%s  (UDP %s)  mode=%s\n", vizHTTPAddr, udpHint, mode)

		err := srv.ListenAndServe(vizHTTPAddr, vizUDPAddr)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		return nil
	},
}

func init() {
	vizCmd.Flags().StringVar(&vizHTTPAddr, "http", ":8257", "HTTP listen address")
	vizCmd.Flags().StringVar(&vizUDPAddr, "udp", "", "UDP listen address (default: http port+1)")
	vizCmd.Flags().BoolVar(&vizListen, "listen", false, "Listen-only mode: skip substrate loop, just serve UI and accept UDP events")
	vizCmd.Flags().StringVar(&vizRepo, "repo", "facebook/babi_qa", "Hugging Face dataset repo")
	vizCmd.Flags().StringVar(&vizSubset, "subset", "en-10k-qa1", "dataset subset / config")
	vizCmd.Flags().StringVar(&vizColumn, "column", "story", "text column to stream")
	vizCmd.Flags().IntVar(&vizIters, "iterations", 80, "number of 1024-byte frames (0 = until dataset EOF)")
	vizCmd.Flags().DurationVar(&vizDelay, "delay", 40*time.Millisecond, "pause between frames (for readability)")

	rootCmd.AddCommand(vizCmd)
}

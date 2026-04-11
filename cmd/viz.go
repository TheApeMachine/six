package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
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

		if vizDemo {
			go runDemo(ctx)
		}

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

/*
runDemo spins up a small mesh and continuously publishes
data through it so the visualizer has real events to display.
*/
func runDemo(ctx context.Context) {
	queue, err := pool.NewQueue(ctx)
	if err != nil {
		errnie.Warn(fmt.Sprintf("viz demo: queue init failed: %v", err))
		return
	}

	defer queue.Close()

	corpus := []struct {
		text  string
		label string
	}{
		{"the cat sat on the mat", "Sentence"},
		{"a quick brown fox jumps over the lazy dog", "Sentence"},
		{"buy now limited offer", "Spam"},
		{"free money click here", "Spam"},
		{"machine learning is fascinating", "Tech"},
		{"quantum computing advances rapidly", "Tech"},
		{"the rain in spain stays mainly in the plain", "Sentence"},
		{"earn cash from home no experience needed", "Spam"},
		{"neural networks approximate any function", "Tech"},
		{"she sells sea shells by the sea shore", "Sentence"},
	}

	idx := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		entry := corpus[idx%len(corpus)]

		values, err := primitive.NewValue([]byte(entry.text))
		if err != nil {
			idx++
			continue
		}

		for _, value := range values {
			publishErr := queue.Publish(value, entry.label)
			if publishErr != nil {
				errnie.Warn(
					fmt.Sprintf(
						"viz demo: Publish failed: %v (label=%q, value=%s)",
						publishErr,
						entry.label,
						value.String(),
					),
				)
			}
		}

		primitive.CloseAll(values)

		idx++

		time.Sleep(200 * time.Millisecond)
	}
}

var vizLong = `
Launches a web-based 3D visualization of the Six system showing nodes,
connections, data flow, field dynamics, and adaptive state in real time.

Use --demo to run a self-contained mesh that generates events.
`

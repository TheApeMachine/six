package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	kernel "github.com/theapemachine/six/kernel"
)

var workerAddr string

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run distributed best-fill worker",
	Long:  "Starts a Cap'n Proto-based worker that serves heterogeneous best-fill shard requests.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if err := kernel.StartDistributedWorker(ctx, workerAddr); err != nil {
			return fmt.Errorf("distributed worker failed: %w", err)
		}
		return nil
	},
}

func init() {
	workerCmd.Flags().StringVar(&workerAddr, "addr", ":7777", "worker listen address")
	rootCmd.AddCommand(workerCmd)
}

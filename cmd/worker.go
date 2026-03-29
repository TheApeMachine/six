package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/distributed"
)

var workerAddr string
var workerAdvertise string
var workerGroup string
var workerIface string
var workerHeartbeat time.Duration
var workerTTL time.Duration
var workerNodeID string

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run a distributed mesh worker",
	Long:  "Runs LAN discovery + remote UniversalBitwise scheduling endpoint.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		if strings.TrimSpace(workerAdvertise) == "" {
			addr, err := distributed.ResolveAdvertiseAddr(workerAddr)
			if err != nil {
				return fmt.Errorf("resolve advertise addr: %w", err)
			}
			workerAdvertise = addr
		}

		discovery := distributed.NewDiscovery(
			ctx,
			distributed.DiscoveryWithNodeID(workerNodeID),
			distributed.DiscoveryWithAdvertiseAddr(workerAdvertise),
			distributed.DiscoveryWithGroup(workerGroup),
			distributed.DiscoveryWithInterface(workerIface),
			distributed.DiscoveryWithHeartbeat(workerHeartbeat),
			distributed.DiscoveryWithTTL(workerTTL),
			distributed.DiscoveryWithCapacity(max(1, runtime.NumCPU()-1)),
		)

		worker, err := distributed.NewWorker(
			ctx,
			distributed.WorkerWithListenAddr(workerAddr),
			distributed.WorkerWithAdvertiseAddr(workerAdvertise),
			distributed.WorkerWithCapacity(max(1, runtime.NumCPU()-1)),
			distributed.WorkerWithDiscovery(discovery),
		)
		if err != nil {
			return err
		}
		defer worker.Close()

		go logMesh(ctx, discovery)
		return worker.ListenAndServe()
	},
}

func logMesh(ctx context.Context, discovery *distributed.Discovery) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			nodes := discovery.Nodes(false)
			fmt.Fprintf(
				os.Stderr,
				"worker mesh peers=%d group=%s advertise=%s\n",
				len(nodes),
				workerGroup,
				workerAdvertise,
			)
		}
	}
}

func init() {
	workerCmd.Flags().StringVar(&workerAddr, "addr", ":7777", "worker HTTP listen address")
	workerCmd.Flags().StringVar(&workerAdvertise, "advertise", "", "advertised host:port for peers (auto-detected when empty)")
	workerCmd.Flags().StringVar(&workerGroup, "mesh-group", distributed.DefaultDiscoveryGroup, "LAN discovery multicast group host:port")
	workerCmd.Flags().StringVar(&workerIface, "mesh-iface", "", "network interface for multicast listener (empty = system default)")
	workerCmd.Flags().DurationVar(&workerHeartbeat, "heartbeat", time.Second, "mesh heartbeat interval")
	workerCmd.Flags().DurationVar(&workerTTL, "ttl", 5*time.Second, "peer expiry timeout")
	workerCmd.Flags().StringVar(&workerNodeID, "node-id", "", "optional stable node id")

	rootCmd.AddCommand(workerCmd)
}

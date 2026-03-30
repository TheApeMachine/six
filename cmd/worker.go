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
var workerShardBits int
var workerShardMask uint64
var workerAutoShardBits int

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

		if workerShardBits < 0 || workerShardBits > 48 {
			return fmt.Errorf("shard-bits must be between 0 and 48")
		}
		if workerAutoShardBits < 0 || workerAutoShardBits > 48 {
			return fmt.Errorf("auto-shard-bits must be between 0 and 48")
		}

		shardBits := uint8(workerShardBits)
		shardMask := workerShardMask & 0x0000FFFFFFFFFFFF

		discovery := distributed.NewDiscovery(
			ctx,
			distributed.DiscoveryWithNodeID(workerNodeID),
			distributed.DiscoveryWithAdvertiseAddr(workerAdvertise),
			distributed.DiscoveryWithGroup(workerGroup),
			distributed.DiscoveryWithInterface(workerIface),
			distributed.DiscoveryWithHeartbeat(workerHeartbeat),
			distributed.DiscoveryWithTTL(workerTTL),
			distributed.DiscoveryWithCapacity(max(1, runtime.NumCPU()-1)),
			distributed.DiscoveryWithAffinityShard(shardMask, shardBits),
		)

		var (
			worker *distributed.Worker
			err    error
		)
		if shardBits > 0 {
			worker, err = distributed.NewWorker(
				ctx,
				distributed.WorkerWithListenAddr(workerAddr),
				distributed.WorkerWithAdvertiseAddr(workerAdvertise),
				distributed.WorkerWithCapacity(max(1, runtime.NumCPU()-1)),
				distributed.WorkerWithDiscovery(discovery),
				distributed.WorkerWithAffinityShard(shardMask, shardBits),
			)
		} else if workerAutoShardBits > 0 {
			worker, err = distributed.NewWorker(
				ctx,
				distributed.WorkerWithListenAddr(workerAddr),
				distributed.WorkerWithAdvertiseAddr(workerAdvertise),
				distributed.WorkerWithCapacity(max(1, runtime.NumCPU()-1)),
				distributed.WorkerWithDiscovery(discovery),
				distributed.WorkerWithAutoAffinityShard(uint8(workerAutoShardBits)),
			)
		} else {
			worker, err = distributed.NewWorker(
				ctx,
				distributed.WorkerWithListenAddr(workerAddr),
				distributed.WorkerWithAdvertiseAddr(workerAdvertise),
				distributed.WorkerWithCapacity(max(1, runtime.NumCPU()-1)),
				distributed.WorkerWithDiscovery(discovery),
			)
		}
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
			self := discovery.Self()
			fmt.Fprintf(
				os.Stderr,
				"worker mesh peers=%d group=%s advertise=%s shard=%s\n",
				len(nodes),
				workerGroup,
				workerAdvertise,
				shardLabel(self.ShardMask, self.ShardBits),
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
	workerCmd.Flags().IntVar(&workerShardBits, "shard-bits", 0, "manual affinity shard prefix width in bits (0 disables manual shard selection)")
	workerCmd.Flags().Uint64Var(&workerShardMask, "shard-mask", 0, "manual affinity shard prefix mask in the top 48 affinity bits (supports 0x...)")
	workerCmd.Flags().IntVar(&workerAutoShardBits, "auto-shard-bits", 0, "auto-assign an affinity shard prefix of this width from node identity when manual shard settings are unset")

	rootCmd.AddCommand(workerCmd)
}

func shardLabel(mask uint64, bits uint8) string {
	if bits == 0 {
		return "unassigned"
	}
	if bits > 48 {
		bits = 48
	}
	prefix := mask >> (48 - bits)
	return fmt.Sprintf("%0*b/%d", bits, prefix, bits)
}

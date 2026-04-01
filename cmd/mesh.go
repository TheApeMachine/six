package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/distributed"
	"github.com/theapemachine/six/pkg/primitive"
)

var meshGroup string
var meshIface string
var meshWait time.Duration
var meshLeft string
var meshRight string

var meshCmd = &cobra.Command{
	Use:   "mesh",
	Short: "Mesh discovery and remote scheduling helpers",
}

var meshNodesCmd = &cobra.Command{
	Use:   "nodes",
	Short: "Discover mesh peers on the local network",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := runMeshDiscovery(cmd.Context())
		if err != nil {
			return err
		}
		defer d.Close()

		nodes := d.Nodes(false)
		if len(nodes) == 0 {
			fmt.Println("no peers discovered")
			return nil
		}
		for _, n := range nodes {
			fmt.Printf("node=%s addr=%s capacity=%d last_seen=%s\n", n.ID, n.Addr, n.Capacity, n.LastSeen.Format(time.RFC3339))
		}
		return nil
	},
}

var meshRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Schedule one UniversalBitwise job on a discovered remote node",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := runMeshDiscovery(cmd.Context())
		if err != nil {
			return err
		}
		defer d.Close()

		left, err := frameFromText(meshLeft)
		if err != nil {
			return err
		}

		var right []byte
		if meshRight != "" {
			right, err = frameFromText(meshRight)
			if err != nil {
				return err
			}
		}

		scheduler := distributed.NewScheduler(d)
		resp, err := scheduler.ScheduleUniversalBitwise(cmd.Context(), left, right)
		if err != nil {
			return err
		}

		out := primitive.BytesToValue(resp.Left)

		fmt.Printf("node=%s duration_ms=%d output=%q\n", resp.NodeID, resp.DurationMS, string(out.Bytes()))
		return nil
	},
}

func runMeshDiscovery(parent context.Context) (*distributed.Discovery, error) {
	ctx, cancel := context.WithTimeout(parent, meshWait)
	defer cancel()

	d := distributed.NewDiscovery(
		ctx,
		distributed.DiscoveryWithGroup(meshGroup),
		distributed.DiscoveryWithInterface(meshIface),
		distributed.DiscoveryWithAnnounce(false),
		distributed.DiscoveryWithHeartbeat(500*time.Millisecond),
		distributed.DiscoveryWithTTL(max(meshWait, 5*time.Second)),
	)
	if err := d.Start(); err != nil {
		return nil, err
	}

	<-ctx.Done()
	return d, nil
}

func frameFromText(text string) ([]byte, error) {
	v, err := primitive.NewValue([]byte(text))
	if err != nil {
		return nil, err
	}
	defer v.Close()

	frame := make([]byte, core.Cfg.Value.Bytes)
	if err := primitive.ValueToBytes(v, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

func init() {
	meshCmd.PersistentFlags().StringVar(&meshGroup, "mesh-group", distributed.DefaultDiscoveryGroup, "LAN discovery multicast group host:port")
	meshCmd.PersistentFlags().StringVar(&meshIface, "mesh-iface", "", "network interface for multicast listener (empty = system default)")
	meshCmd.PersistentFlags().DurationVar(&meshWait, "wait", 3*time.Second, "discovery wait duration")

	meshRunCmd.Flags().StringVar(&meshLeft, "left", "hello", "left frame token text")
	meshRunCmd.Flags().StringVar(&meshRight, "right", "", "right frame token text (optional)")

	meshCmd.AddCommand(meshNodesCmd)
	meshCmd.AddCommand(meshRunCmd)
	rootCmd.AddCommand(meshCmd)
}

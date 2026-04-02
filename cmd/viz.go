package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/vm"
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

// vizMachinePool hands out vm.Machine instances for concurrent prompt/ingest
// handling; each goroutine releases its machine back to the pool when done.
type vizMachinePool struct {
	mu   sync.Mutex
	idle []*vm.Machine
	newM func() (*vm.Machine, error)
}

func (p *vizMachinePool) acquire() (*vm.Machine, error) {
	p.mu.Lock()
	n := len(p.idle)
	if n > 0 {
		m := p.idle[n-1]
		p.idle = p.idle[:n-1]
		p.mu.Unlock()
		return m, nil
	}
	p.mu.Unlock()
	return p.newM()
}

func (p *vizMachinePool) release(m *vm.Machine) {
	if m == nil {
		return
	}
	p.mu.Lock()
	p.idle = append(p.idle, m)
	p.mu.Unlock()
}

func (p *vizMachinePool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.idle {
		_ = m.Close()
	}
	p.idle = nil
}

var vizCmd = &cobra.Command{
	Use:   "viz",
	Short: "Run the 3D substrate visualizer with live telemetry",
	Long: `Starts the HTTP/WebSocket visualizer and optionally runs the experiment
substrate loop. Use --listen to skip the dataset loop and only serve the UI,
accepting graph events via UDP from external test runs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv := visualizer.NewServer()

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		mode := "substrate"
		if vizListen {
			mode = "listen-only (send events via UDP)"
		}

		var pool *vizMachinePool
		if !vizListen {
			pool = &vizMachinePool{
				newM: func() (*vm.Machine, error) {
					return vm.NewMachine(
						ctx,
						vm.WithSources(huggingface.New(
							huggingface.DatasetWithContext(ctx),
							huggingface.DatasetWithRepo(vizRepo),
							huggingface.DatasetWithSubset(vizSubset),
							huggingface.DatasetWithTextColumns(vizColumn),
						)),
					)
				},
			}
			defer pool.closeAll()

			runFrame := func(raw []byte) ([]byte, error) {
				machine, err := pool.acquire()
				if err != nil {
					return nil, err
				}
				defer pool.release(machine)

				observer := tools.NewObserver(machine)
				defer observer.Close()

				value, err := primitive.NewValue(raw)
				if err != nil {
					return nil, err
				}
				defer value.Close()

				idx := core.Cfg.Value.Region.State.Index
				if idx >= 0 && idx < len(value) {
					value[idx] = 1
				}

				if _, err := io.Copy(observer, value); err != nil {
					return nil, err
				}

				observedFrame := make([]byte, core.Cfg.Value.Bytes)
				if _, err := io.ReadFull(observer, observedFrame); err != nil {
					return nil, err
				}

				return observedFrame, nil
			}

			srv.SetPromptFunc(func(msg string) ([]byte, error) {
				return runFrame([]byte(msg))
			})
			srv.SetIngestFunc(func(raw []byte) error {
				_, err := runFrame(raw)
				return err
			})
		}

		go func() {
			<-ctx.Done()
			srv.Shutdown()
		}()

		udpHint := "auto"
		if vizUDPAddr != "" {
			udpHint = vizUDPAddr
		}

		fmt.Fprintf(os.Stderr, "visualizer http://%s  (UDP %s)  mode=%s\n", vizHTTPAddr, udpHint, mode)

		if err := srv.ListenAndServe(
			vizHTTPAddr, vizUDPAddr,
		); err != nil && !errors.Is(
			err, http.ErrServerClosed,
		) {
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

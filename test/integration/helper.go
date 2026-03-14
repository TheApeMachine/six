package integration

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/process"
	"github.com/theapemachine/six/pkg/system/vm"
	"github.com/theapemachine/six/pkg/telemetry"
)

/*
IntegrationHelper boots a full vm.Machine with the real system stack.
Tests interact exclusively through machine.Prompt and machine.Stop.
*/
type IntegrationHelper struct {
	ctx             context.Context
	cancel          context.CancelFunc
	Machine         *vm.Machine
	promptTokenizer *process.TokenizerServer
	Events          chan telemetry.Event
	telemetryConn   *net.UDPConn
}

type BoundaryProbe struct {
	Query    string
	Terminal string
	Next     string
}

/*
NewIntegrationHelper constructs the full system and blocks until the
spatial index is populated. The returned Machine is ready for Prompt calls.
*/
func NewIntegrationHelper(
	ctx context.Context,
	dataset provider.Dataset,
) *IntegrationHelper {
	ctx, cancel := context.WithCancel(ctx)

	// Boot random UDP telemetry listener so tests can introspect Fold pipeline
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		panic("ResolveUDPAddr: " + err.Error())
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		panic("ListenUDP: " + err.Error())
	}

	events := make(chan telemetry.Event, 5000)

	go func() {
		buf := make([]byte, 8192)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				n, _, err := conn.ReadFromUDP(buf)
				if err != nil {
					continue
				}

				var event telemetry.Event
				if err := json.Unmarshal(buf[:n], &event); err == nil {
					if event.Action == "Fold" {
						events <- event
					}
				}
			}
		}
	}()

	machine := vm.NewMachine(
		vm.MachineWithContext(ctx),
		vm.MachineWithSystems(
			lsm.NewSpatialIndexServer(
				lsm.WithContext(ctx),
			),
			process.NewTokenizerServer(
				process.TokenizerWithContext(ctx),
				process.TokenizerWithDataset(dataset, false),
			),
			substrate.NewGraphServer(
				substrate.GraphWithContext(ctx),
				substrate.GraphWithSink(
					telemetry.NewSink(telemetry.WithAddress(conn.LocalAddr().String())),
				),
			),
		),
	)

	machine.Start()

	promptTokenizer := process.NewTokenizerServer(
		process.TokenizerWithContext(ctx),
		process.TokenizerWithDataset(dataset, true),
		process.TokenizerWithCollector(make([][]data.Chord, 1)),
	)

	promptTokenizer.Start(machine.Pool(), nil)

	return &IntegrationHelper{
		ctx:             ctx,
		cancel:          cancel,
		Machine:         machine,
		promptTokenizer: promptTokenizer,
		Events:          events,
		telemetryConn:   conn,
	}
}

/*
NewPrompt creates a process.Prompt wired to the real tokenizer, ready
to be passed to machine.Prompt.
*/
func (helper *IntegrationHelper) NewPrompt(queries []string) *process.Prompt {
	return process.NewPrompt(
		process.PromptWithStrings(queries),
		process.PromptWithTokenizer(helper.promptTokenizer),
	)
}

/*
ContainsExpected iterates over results and returns true if any result
matches the expected string.
*/
func (helper *IntegrationHelper) ContainsExpected(results [][]byte, expected string) bool {
	for _, result := range results {
		if string(result) == expected {
			return true
		}
	}
	return false
}

func (helper *IntegrationHelper) ContainsAny(results [][]byte, expected ...string) bool {
	for _, candidate := range expected {
		if helper.ContainsExpected(results, candidate) {
			return true
		}
	}

	return false
}

func (helper *IntegrationHelper) ResultsBelongToChunks(results [][]byte, chunks []string) bool {
	allowed := make(map[string]struct{}, len(chunks))

	for _, chunk := range chunks {
		allowed[chunk] = struct{}{}
	}

	for _, result := range results {
		if _, ok := allowed[string(result)]; !ok {
			return false
		}
	}

	return true
}

func ResultStrings(results [][]byte) []string {
	out := make([]string, 0, len(results))

	for _, result := range results {
		out = append(out, string(result))
	}

	return out
}

/*
pollUntil runs check every interval until it returns true or timeout elapses.
*/
func pollUntil(timeout, interval time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(interval)
	}

	return false
}

func ChunkStrings(sample string) []string {
	sequencer := process.NewSeq(process.NewCalibrator())
	raw := []byte(sample)
	chunk := make([]byte, 0, len(raw))
	chunks := make([]string, 0, len(raw))

	flush := func(width int) {
		if width == 0 {
			return
		}

		chunks = append(chunks, string(append([]byte(nil), chunk[:width]...)))
		copy(chunk, chunk[width:])
		chunk = chunk[:len(chunk)-width]
	}

	for idx, symbol := range raw {
		chunk = append(chunk, symbol)

		isBoundary, emitWidth, _, _ := sequencer.Analyze(uint32(idx), symbol)
		if isBoundary {
			flush(emitWidth)
		}
	}

	for {
		isBoundary, emitWidth, _, _ := sequencer.Flush()
		if !isBoundary {
			break
		}

		flush(emitWidth)
	}

	if len(chunk) > 0 {
		chunks = append(chunks, string(chunk))
	}

	return chunks
}

func BoundaryProbes(sample string) []BoundaryProbe {
	chunks := ChunkStrings(sample)
	probes := make([]BoundaryProbe, 0, max(len(chunks)-1, 0))
	var prefix strings.Builder

	for idx := 0; idx < len(chunks)-1; idx++ {
		prefix.WriteString(chunks[idx])

		probes = append(probes, BoundaryProbe{
			Query:    prefix.String(),
			Terminal: chunks[idx],
			Next:     chunks[idx+1],
		})
	}

	return probes
}

/*
Teardown cancels the context and stops the machine.
*/
func (helper *IntegrationHelper) Teardown() {
	if helper.cancel != nil {
		helper.cancel()
		helper.cancel = nil
	}
	if helper.telemetryConn != nil {
		helper.telemetryConn.Close()
	}
	helper.Machine.Stop()
}

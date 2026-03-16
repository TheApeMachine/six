package kernel

import (
	"context"
	"testing"
	"time"

	config "github.com/theapemachine/six/pkg/system/core"
)

func TestStartDiscoveryCanRestartAfterContextCancellation(t *testing.T) {
	originalWorkers := append([]string(nil), config.System.Workers...)
	defer func() {
		config.System.Workers = originalWorkers
		configuredWorkersMu.Lock()
		configuredWorkers = nil
		configuredWorkersMu.Unlock()
		discoveryMu.Lock()
		discoveryState = nil
		discoveryMu.Unlock()
	}()

	ctx1, cancel1 := context.WithCancel(context.Background())
	StartDiscovery(ctx1, ":0")
	if len(config.System.Workers) == 0 {
		t.Fatalf("expected local worker after first discovery start")
	}
	cancel1()
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		discoveryMu.Lock()
		stopped := discoveryState == nil
		discoveryMu.Unlock()
		if stopped {
			break
		}
	}

	discoveryMu.Lock()
	if discoveryState != nil {
		discoveryMu.Unlock()
		t.Fatalf("expected discovery state to clear after cancellation")
	}
	discoveryMu.Unlock()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer func() {
		cancel2()
		for i := 0; i < 20; i++ {
			time.Sleep(10 * time.Millisecond)
			discoveryMu.Lock()
			stopped := discoveryState == nil
			discoveryMu.Unlock()
			if stopped {
				break
			}
		}
	}()
	StartDiscovery(ctx2, ":0")
	if len(config.System.Workers) == 0 {
		t.Fatalf("expected local worker after discovery restart")
	}

	discoveryMu.Lock()
	active := discoveryState != nil && discoveryState.ctx == ctx2
	discoveryMu.Unlock()
	if !active {
		t.Fatalf("expected restarted discovery state to track the second context")
	}
}

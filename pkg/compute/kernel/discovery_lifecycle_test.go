package kernel

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/system/core"
)

func TestStartDiscoveryCanRestartAfterContextCancellation(t *testing.T) {
	Convey("Given a fresh discovery configuration", t, func() {
		originalWorkers := append([]string(nil), config.System.Workers...)
		Reset(func() {
			config.System.Workers = originalWorkers
			configuredWorkersMu.Lock()
			configuredWorkers = nil
			configuredWorkersMu.Unlock()
			discoveryMu.Lock()
			discoveryState = nil
			discoveryMu.Unlock()
		})

		Convey("When starting discovery and terminating first context", func() {
			ctx1, cancel1 := context.WithCancel(context.Background())
			StartDiscovery(ctx1)
			
			So(len(config.System.Workers), ShouldBeGreaterThan, 0)
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
			stopped := discoveryState == nil
			discoveryMu.Unlock()
			So(stopped, ShouldBeTrue)

			Convey("It should restart cleanly with the second context", func() {
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
				StartDiscovery(ctx2)
				
				if len(config.System.Workers) == 0 {
					t.Fatalf("expected local worker after discovery restart")
				}

				discoveryMu.Lock()
				active := discoveryState != nil && discoveryState.ctx == ctx2
				discoveryMu.Unlock()
				So(active, ShouldBeTrue)
			})
		})
	})
}

func BenchmarkStartDiscoveryCanRestartAfterContextCancellation(b *testing.B) {
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

	for b.Loop() {
		config.System.Workers = nil
		configuredWorkersMu.Lock()
		configuredWorkers = nil
		configuredWorkersMu.Unlock()
		discoveryMu.Lock()
		discoveryState = nil
		discoveryMu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		StartDiscovery(ctx)
		cancel()
		for j := 0; j < 20; j++ {
			time.Sleep(10 * time.Millisecond)
			discoveryMu.Lock()
			stopped := discoveryState == nil
			discoveryMu.Unlock()
			if stopped {
				break
			}
		}
	}
}

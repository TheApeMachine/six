package vm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/program"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/telemetry"
)

const machineConfigEnv = "CONFIG_PATH"
const machineDefaultConfigPath = "cmd/cfg/config.yml"

var machineConfigOnce sync.Once
var machineConfigErr error

func TestMachineCycleBootstrapsCommunityRecruiter(t *testing.T) {
	Convey("Given unassigned Values without installed programs", t, func() {
		loadMachineConfig(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		backend := compute.NewBackend(ctx)
		defer backend.Close()

		first := primitive.Emit()
		second := primitive.Emit()
		start, _ := primitive.AffinityRegion.WordExtent()

		first.Set(start, 0b0011)
		first.NormalizeAffinity()
		first.SetStatus(primitive.ERROR)
		second.Set(start, 0b0100)
		second.NormalizeAffinity()
		second.SetStatus(primitive.ERROR)

		machine := &Machine{
			ctx:       ctx,
			cancel:    cancel,
			backend:   backend,
			community: []*primitive.Value{first, second},
		}
		defer func() {
			primitive.CloseAll(machine.community)
		}()

		Convey("When the machine cycles", func() {
			_, err := machine.Cycle()

			Convey("It should emit one in-value recruiter and let firmware stamp the community", func() {
				So(err, ShouldBeNil)
				So(len(machine.community), ShouldEqual, 3)

				recruiter := machine.community[2]
				recruiterCommunity, communityErr := recruiter.Property(primitive.COMMUNITY)
				firstCommunity, firstErr := first.Property(primitive.COMMUNITY)
				secondCommunity, secondErr := second.Property(primitive.COMMUNITY)

				So(communityErr, ShouldBeNil)
				So(firstErr, ShouldBeNil)
				So(secondErr, ShouldBeNil)
				So(recruiterCommunity, ShouldEqual, recruiter.ID())
				So(firstCommunity, ShouldEqual, recruiter.ID())
				So(secondCommunity, ShouldEqual, recruiter.ID())
				So(first.Status(), ShouldEqual, primitive.PENDING)
				So(second.Status(), ShouldEqual, primitive.PENDING)
				So(recruiter.HasProgram(), ShouldBeFalse)
				So(recruiter.Status(), ShouldEqual, primitive.DONE)
			})
		})
	})
}

func TestMachineCycleEmitsNextRecruiterAfterShannonCap(t *testing.T) {
	Convey("Given an unassigned Value outside the recruiter's Shannon cap (120 low bits vs sibling at 119)", t, func() {
		loadMachineConfig(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		backend := compute.NewBackend(ctx)
		defer backend.Close()

		first := primitive.Emit()
		defer first.Close()
		setMachineAffinityPrefix(first, 120)

		second := primitive.Emit()
		defer second.Close()
		setMachineAffinityBit(second, 119)

		machine := &Machine{
			ctx:       ctx,
			cancel:    cancel,
			backend:   backend,
			community: []*primitive.Value{first, second},
		}
		defer func() {
			for _, value := range machine.community[2:] {
				value.Close()
			}
		}()

		Convey("When the machine cycles twice", func() {
			_, firstErr := machine.Cycle()
			_, secondErr := machine.Cycle()

			Convey("It should leave the saturated Value for a new recruiter", func() {
				So(firstErr, ShouldBeNil)
				So(secondErr, ShouldBeNil)
				So(len(machine.community), ShouldEqual, 4)

				firstRecruiter := machine.community[2]
				secondRecruiter := machine.community[3]
				firstCommunity, firstCommunityErr := first.Property(primitive.COMMUNITY)
				secondCommunity, secondCommunityErr := second.Property(primitive.COMMUNITY)

				So(firstCommunityErr, ShouldBeNil)
				So(secondCommunityErr, ShouldBeNil)
				So(firstCommunity, ShouldEqual, firstRecruiter.ID())
				So(secondCommunity, ShouldEqual, secondRecruiter.ID())
				So(firstRecruiter.ID(), ShouldNotEqual, secondRecruiter.ID())
			})
		})
	})
}

func TestMachineCyclePublishesChangedTelemetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url, messages, closeServer := newMachineTelemetryServer(t)
	defer closeServer()

	enabled := core.Cfg.TelemetryEnabled
	defer func() {
		core.Cfg.TelemetryEnabled = enabled
	}()
	core.Cfg.TelemetryEnabled = true

	bridge, err := telemetry.NewBridge(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()

	backend := compute.NewBackend(ctx)
	defer backend.Close()

	value := primitive.Emit()
	defer value.Close()
	value.SetProperty(primitive.COMMUNITY, 1)

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		backend:   backend,
		telemetry: bridge,
		community: []*primitive.Value{value},
	}

	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}
	if got := readMachineTelemetryMessage(t, messages); got != primitive.FrameByteLength {
		t.Fatalf("telemetry frame length = %d, want %d", got, primitive.FrameByteLength)
	}

	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}
	if !noMachineTelemetryMessage(messages) {
		t.Fatal("unchanged Value was resent")
	}

	value.SetProperty(primitive.NOISE, 1)
	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}
	if got := readMachineTelemetryMessage(t, messages); got != primitive.FrameByteLength {
		t.Fatalf("changed telemetry frame length = %d, want %d", got, primitive.FrameByteLength)
	}
}

func TestMachineCyclePrunesExpiredEphemeralValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := compute.NewBackend(ctx)
	defer backend.Close()

	compiled, err := program.Compile(`[ (signals[0,1] self) <= (id) <= community ]`, program.Layout{
		Regions: map[string]program.RegionExtent{
			"id":      {Start: primitive.IDStartWord, Words: primitive.IDWords},
			"signals": {Start: primitive.SignalsStartWord, Words: primitive.SignalsWords},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	value := primitive.Emit(primitive.WithTTL(1))
	if !value.InstallProgram(compiled.Words) {
		value.Close()
		t.Fatal("failed to install ephemeral program")
	}

	machine := &Machine{
		ctx:       ctx,
		cancel:    cancel,
		backend:   backend,
		community: []*primitive.Value{value},
	}

	if _, err := machine.Cycle(); err != nil {
		t.Fatal(err)
	}

	if len(machine.community) != 0 {
		primitive.CloseAll(machine.community)
		t.Fatalf("community len = %d, want expired ephemeral pruned", len(machine.community))
	}
}

func TestMachinePromptAppendsValuesBeforeCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	backend := compute.NewBackend(ctx)
	defer backend.Close()

	value := primitive.Emit()
	defer value.Close()
	value.SetProperty(primitive.COMMUNITY, 1)
	value.SetStatus(primitive.DONE)

	machine := &Machine{
		ctx:     ctx,
		cancel:  cancel,
		backend: backend,
	}

	resolved, err := machine.Prompt(value)
	if err != nil {
		t.Fatal(err)
	}

	if len(machine.community) != 1 {
		t.Fatalf("community len = %d, want 1", len(machine.community))
	}
	if len(resolved) != 1 || resolved[0] != value {
		t.Fatalf("resolved len = %d, want prompt Value resolved", len(resolved))
	}
}

func newMachineTelemetryServer(t *testing.T) (string, <-chan int, func()) {
	t.Helper()

	messages := make(chan int, 8)
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}

			messages <- len(payload)
		}
	}))

	return "ws" + strings.TrimPrefix(server.URL, "http"), messages, server.Close
}

func readMachineTelemetryMessage(t *testing.T, messages <-chan int) int {
	t.Helper()

	select {
	case length := <-messages:
		return length
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for telemetry message")
	}

	return 0
}

func noMachineTelemetryMessage(messages <-chan int) bool {
	select {
	case <-messages:
		return false
	case <-time.After(50 * time.Millisecond):
		return true
	}
}

func setMachineAffinityPrefix(value *primitive.Value, bitCount int) {
	if value == nil {
		return
	}

	for bit := 0; bit < bitCount && bit < primitive.AffinityBits; bit++ {
		word := primitive.AffinityStartWord + (bit / 64)
		value.Set(word, (*value)[word]|uint64(1<<(bit%64)))
	}

	value.NormalizeAffinity()
}

func setMachineAffinityBit(value *primitive.Value, bit int) {
	if value == nil || bit < 0 || bit >= primitive.AffinityBits {
		return
	}

	word := primitive.AffinityStartWord + (bit / 64)
	value.Set(word, (*value)[word]|uint64(1<<(bit%64)))
	value.NormalizeAffinity()
}

func loadMachineConfig(t *testing.T) {
	t.Helper()

	machineConfigOnce.Do(func() {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			machineConfigErr = errors.New("cannot resolve vm test file")
			return
		}

		viper.SetConfigFile(machineConfigPath(file))
		machineConfigErr = viper.ReadInConfig()
		if machineConfigErr != nil {
			return
		}

		core.Cfg = core.NewConfig()
	})

	if machineConfigErr != nil {
		t.Fatalf("load machine config: %v", machineConfigErr)
	}
}

func machineConfigPath(file string) string {
	if configured := os.Getenv(machineConfigEnv); configured != "" {
		return filepath.Clean(configured)
	}

	if filepath.IsAbs(machineDefaultConfigPath) {
		return filepath.Clean(machineDefaultConfigPath)
	}

	if _, err := os.Stat(machineDefaultConfigPath); err == nil {
		return filepath.Clean(machineDefaultConfigPath)
	}

	return filepath.Clean(filepath.Join(
		filepath.Dir(file),
		"..", "..",
		"cmd", "cfg", "config.yml",
	))
}

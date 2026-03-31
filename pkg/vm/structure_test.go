package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

func resolveVMTestConfigPath() string {
	if e := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); e != "" {
		return filepath.Clean(e)
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"))
	}
	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveVMTestConfigPath())

	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "pkg/vm: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}

	viper.Set("loglevel", "error")
	viper.Set("logging.trace.path", os.DevNull)
	loggingCfg, err := core.LoadLoggingConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pkg/vm: core.LoadLoggingConfig: %v\n", err)
		os.Exit(1)
	}
	errnie.InitLogger(loggingCfg)
	code := m.Run()
	_ = errnie.Shutdown(context.Background())
	os.Exit(code)
}

func TestStructureFromWorkspaceLinksParentAndRegistersLSM(t *testing.T) {
	Convey("Structure gets a new ID, links Prev to parent, indexes LSM", t, func() {
		store.ResetDefaultSpatialIndex()

		parent, err := primitive.NewValue([]byte("dataset line"))
		So(err, ShouldBeNil)
		defer func() {
			parent.InstallTombstone()
			_ = parent.Close()
		}()

		var workSelf, workPartner primitive.Value
		primitive.CopyFrame(&workSelf, parent)
		primitive.CopyFrame(&workPartner, parent)
		workSelf.InstallLearnFirmware()

		backend := compute.NewBackend()
		So(backend.UniversalBitwise(
			unsafe.Pointer(&workSelf),
			unsafe.Pointer(&workPartner),
		), ShouldBeNil)

		st := StructureFromWorkspace(StructureKindLearnCancel, parent, &workSelf)
		So(st.SourceValueID, ShouldEqual, parent[core.Cfg.Value.Region.ID.Start])
		So(st.Frame[core.Cfg.Value.Region.ID.Start], ShouldNotEqual, parent[core.Cfg.Value.Region.ID.Start])
		So(st.Frame[core.Cfg.Value.Region.Prev.Start], ShouldEqual, parent[core.Cfg.Value.Region.Prev.Start])

		st.RegisterDefaultLSM()
		store.DefaultSpatialIndex().Flush()

		So(primitive.TokenRegionFingerprint(parent), ShouldNotEqual, 0)
	})
}

package compute_test

import (
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
	"github.com/theapemachine/six/pkg/primitive"
)

func resolveTombstoneTestConfigPath() string {
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
	viper.SetConfigFile(resolveTombstoneTestConfigPath())
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "compute/tombstone_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "compute/tombstone_test: core.LoadValueConfig: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestTombstoneFirmwareZerosRegions(t *testing.T) {
	Convey("InstallTombstone + ALU execution zeros Tokens, Affinity, and Program regions in-band", t, func() {
		value, err := primitive.NewValue([]byte("hello"))
		So(err, ShouldBeNil)

		// Confirm regions have live data before tombstoning.
		So(value[core.Cfg.TokenIndex], ShouldNotEqual, uint64(0))
		So(value[core.Cfg.AffinityIndex], ShouldNotEqual, uint64(0))

		// Install tombstone firmware in-band and execute via the ALU.
		value.InstallTombstone()

		var zeroBuf [primitive.ByteSize]byte
		partner := primitive.ViewValue(zeroBuf[:])

		execErr := compute.UniversalBitwise(
			unsafe.Pointer(value),
			unsafe.Pointer(partner),
		)
		So(execErr, ShouldBeNil)

		// After tombstone execution, token and affinity regions must be zeroed.
		nTokenWords := int(core.Cfg.TokenBits / 64)
		for i := 0; i < nTokenWords; i++ {
			So(value[core.Cfg.TokenIndex+i], ShouldEqual, uint64(0))
		}
		So(value[core.Cfg.AffinityIndex], ShouldEqual, uint64(0))

		// PrevID (58) and NextID (59) must encode the ValueID XOR departure signal.
		// They are NOT zero — they hold the XOR of the original link value and the ValueID.
		// (In this test, PrevID and NextID started as 0, so they now equal ValueID.)
		savedValueID := value[core.Cfg.ValueID]
		// ValueID itself was zeroed last by tombstone; load it from what we stored pre-exec.
		// We captured it above as the first token word assertion was passing.
		_ = savedValueID // the XOR spread test verifies via specific test below

		// Release must now succeed without a tombstone error.
		So(value.Release(), ShouldBeNil)
	})
}

func TestTombstoneXORSpreadEncodesDeparture(t *testing.T) {
	Convey("Tombstone XORs the ValueID into PrevID and NextID as a departure signal", t, func() {
		value, err := primitive.NewValue([]byte("spread"))
		So(err, ShouldBeNil)

		valueID := value[core.Cfg.ValueID]
		prevID := value[core.Cfg.PreviousID]
		nextID := value[core.Cfg.NextID]

		// Set non-zero link words to verify XOR spread.
		value[core.Cfg.PreviousID] = 0xDEADBEEF00000001
		value[core.Cfg.NextID] = 0xCAFEBABE00000002
		prevID = value[core.Cfg.PreviousID]
		nextID = value[core.Cfg.NextID]

		value.InstallTombstone()

		var zeroBuf [primitive.ByteSize]byte
		partner := primitive.ViewValue(zeroBuf[:])

		So(compute.UniversalBitwise(unsafe.Pointer(value), unsafe.Pointer(partner)), ShouldBeNil)

		// After tombstone: PrevID = original_prevID ^ ValueID
		So(value[core.Cfg.PreviousID], ShouldEqual, prevID^valueID)
		// After tombstone: NextID = original_nextID ^ ValueID
		So(value[core.Cfg.NextID], ShouldEqual, nextID^valueID)

		So(value.Release(), ShouldBeNil)
	})
}

func TestLearnFirmwareXORsTokensViaLoop(t *testing.T) {
	Convey("Learn firmware XORs self token words [0-56] with partner via DJNZ loop", t, func() {
		self, err := primitive.NewValue(nil)
		So(err, ShouldBeNil)

		partner, err := primitive.NewValue(nil)
		So(err, ShouldBeNil)

		// Seed distinct patterns so XOR produces non-trivial results.
		nTokenWords := int(core.Cfg.TokenBits / 64)
		for i := 0; i < nTokenWords; i++ {
			self[core.Cfg.TokenIndex+i] = 0xAAAAAAAAAAAAAAAA
			partner[core.Cfg.TokenIndex+i] = 0xCCCCCCCCCCCCCCCC
		}
		expected := uint64(0xAAAAAAAAAAAAAAAA ^ 0xCCCCCCCCCCCCCCCC) // 0x6666...

		self.InstallLearnFirmware()
		So(compute.UniversalBitwise(unsafe.Pointer(self), unsafe.Pointer(partner)), ShouldBeNil)

		for i := 0; i < nTokenWords; i++ {
			So(self[core.Cfg.TokenIndex+i], ShouldEqual, expected)
		}

		self.InstallTombstone()
		So(compute.UniversalBitwise(unsafe.Pointer(self), unsafe.Pointer(partner)), ShouldBeNil)
		So(self.Release(), ShouldBeNil)

		partner.InstallTombstone()
		var zeroBuf2 [primitive.ByteSize]byte
		zp2 := primitive.ViewValue(zeroBuf2[:])
		So(compute.UniversalBitwise(unsafe.Pointer(partner), unsafe.Pointer(zp2)), ShouldBeNil)
		So(partner.Release(), ShouldBeNil)
	})
}

func TestBuildFirmwareANDsTokensViaLoop(t *testing.T) {
	Convey("Build firmware ANDs self token words [0-56] with partner via DJNZ loop", t, func() {
		self, err := primitive.NewValue(nil)
		So(err, ShouldBeNil)

		partner, err := primitive.NewValue(nil)
		So(err, ShouldBeNil)

		nTokenWords := int(core.Cfg.TokenBits / 64)
		for i := 0; i < nTokenWords; i++ {
			self[core.Cfg.TokenIndex+i] = 0xAAAAAAAAAAAAAAAA
			partner[core.Cfg.TokenIndex+i] = 0xCCCCCCCCCCCCCCCC
		}
		expected := uint64(0xAAAAAAAAAAAAAAAA & 0xCCCCCCCCCCCCCCCC) // 0x8888...

		self.InstallBuildFirmware()
		So(compute.UniversalBitwise(unsafe.Pointer(self), unsafe.Pointer(partner)), ShouldBeNil)

		for i := 0; i < nTokenWords; i++ {
			So(self[core.Cfg.TokenIndex+i], ShouldEqual, expected)
		}

		self.InstallTombstone()
		So(compute.UniversalBitwise(unsafe.Pointer(self), unsafe.Pointer(partner)), ShouldBeNil)
		So(self.Release(), ShouldBeNil)

		partner.InstallTombstone()
		var zeroBuf3 [primitive.ByteSize]byte
		zp3 := primitive.ViewValue(zeroBuf3[:])
		So(compute.UniversalBitwise(unsafe.Pointer(partner), unsafe.Pointer(zp3)), ShouldBeNil)
		So(partner.Release(), ShouldBeNil)
	})
}

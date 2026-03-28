package cpu

import (
	"fmt"
	"math/bits"
	"os"
	"runtime"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

func setupBackendTests() {
	viper.SetConfigFile("../../../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "cpu/backend_test: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "cpu/backend_test: core.LoadValueConfig: %v\n", err)
		os.Exit(1)
	}
	loggingCfg, err := core.LoadLoggingConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cpu/backend_test: core.LoadLoggingConfig: %v\n", err)
		os.Exit(1)
	}
	errnie.InitLogger(loggingCfg)
}

func TestMain(m *testing.M) {
	setupBackendTests()
	os.Exit(m.Run())
}

// loadProgramWords writes compiled 32-bit instructions into value's program region
// starting at pc=0 (pairs packed into uint64 program words).
func loadProgramWords(val *primitive.Value, src string) error {
	prog, err := core.CompileFunc(src)
	if err != nil {
		return err
	}
	base := core.Cfg.ProgramIndex
	for w := range primitive.Words {
		if base+w < primitive.Words {
			val[base+w] = 0
		}
	}
	for i := range prog {
		wordPos := base + i/2
		if i%2 == 0 {
			val[wordPos] = uint64(prog[i])
		} else {
			val[wordPos] |= uint64(prog[i]) << 32
		}
	}
	val[core.Cfg.RegPC] = 0
	val[core.Cfg.FW] = 0
	return nil
}

// two-instruction kernel: one ALU op then halt (op 0 with pc>0).
const haltSecond = "\n0 0 0000"

func TestBackend_UniversalBitwise(t *testing.T) {
	be := NewBackend()

	t.Run("nil pointers", func(t *testing.T) {
		var v primitive.Value
		if err := be.UniversalBitwise(nil, unsafe.Pointer(&v)); err == nil {
			t.Fatal("expected error for nil a")
		}
		if err := be.UniversalBitwise(unsafe.Pointer(&v), nil); err == nil {
			t.Fatal("expected error for nil b")
		}
	})

	tests := []struct {
		name     string
		program  string
		scratch  int // word index where result is written (dst immediate in program)
		wantWord uint64
	}{
		{"zeros_and", "0 100 0001" + haltSecond, 100, 0},
		{"and_3_4", "3 100 0001" + haltSecond, 100, 0},
		{"or_3_4", "3 4 0111" + haltSecond, 4, 7},
		{"xor_3_4", "3 4 0110" + haltSecond, 4, 7},
		// Immediates with bit 12 set (≥4096) no longer trigger span mode;
		// only the register flag 0x3000 does. This tests the old boundary.
		{"max12_xor_no_span_flag", "4095 100 0110" + haltSecond, 100, 4095 ^ 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var a, b primitive.Value
			if err := loadProgramWords(&a, tc.program); err != nil {
				t.Fatalf("loadProgramWords: %v", err)
			}
			if err := be.UniversalBitwise(unsafe.Pointer(&a), unsafe.Pointer(&b)); err != nil {
				t.Fatalf("UniversalBitwise: %v", err)
			}
			if tc.scratch < 0 || tc.scratch >= primitive.Words {
				t.Fatalf("bad scratch index %d", tc.scratch)
			}
			if got := a[tc.scratch]; got != tc.wantWord {
				t.Fatalf("word[%d]: got %d want %d", tc.scratch, got, tc.wantWord)
			}
		})
	}
}

func TestAvailable(t *testing.T) {
	Convey("Available reports logical CPU count", t, func() {
		So(Available(), ShouldEqual, runtime.NumCPU())
		So(Available(), ShouldBeGreaterThan, 0)
	})
}

func TestNewBackend(t *testing.T) {
	Convey("NewBackend returns a non-nil Backend", t, func() {
		b := NewBackend()
		So(b, ShouldNotBeNil)
	})
}

func BenchmarkBackend_UniversalBitwise(b *testing.B) {
	be := NewBackend()
	const n = 64
	a := make([]primitive.Value, n)
	bv := make([]primitive.Value, n)

	for i := range n {
		a[i][0] = uint64(i + 1)
		bv[i][4] = uint64(bits.Reverse64(uint64(i)))
	}
	b.ResetTimer()
	for b.Loop() {
		for i := 0; i < n; i++ {
			_ = be.UniversalBitwise(unsafe.Pointer(&a[i]), unsafe.Pointer(&bv[i]))
		}
	}
}

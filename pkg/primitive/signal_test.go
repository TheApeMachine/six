package primitive

import (
	"testing"

	"github.com/theapemachine/six/pkg/core"
)

func TestLongestZeroRun(t *testing.T) {
	// 64 bits all-one → no zero run.
	words := []uint64{^uint64(0)}
	start, length := LongestZeroRun(words, 1)
	if length != 0 {
		t.Fatalf("expected zero run length 0, got %d at %d", length, start)
	}

	// 64 bits all-zero → zero run of 64.
	words = []uint64{0}
	start, length = LongestZeroRun(words, 1)
	if length != 64 {
		t.Fatalf("expected zero run length 64, got %d at %d", length, start)
	}

	// 0xFF00...00 = high byte set, rest zero → 56-bit zero run starting at bit 0.
	words = []uint64{0xFF00000000000000}
	start, length = LongestZeroRun(words, 1)
	if length != 56 || start != 0 {
		t.Fatalf("expected 56-bit zero run at bit 0, got %d at %d", length, start)
	}
}

func TestLongestOneRun(t *testing.T) {
	words := []uint64{0xFF} // 8 consecutive 1-bits at the low end.
	start, length := LongestOneRun(words, 1)
	if length != 8 || start != 0 {
		t.Fatalf("expected 8-bit one run at 0, got %d at %d", length, start)
	}
}

func TestExtractSpan(t *testing.T) {
	words := []uint64{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF}
	span := ExtractSpan(words, 60, 8)
	// Bits 60-67 from all-ones should give 0xFF when packed from bit 0.
	if len(span) != 1 {
		t.Fatalf("expected 1 word, got %d", len(span))
	}
	if span[0] != 0xFF {
		t.Fatalf("expected 0xFF, got 0x%X", span[0])
	}
}

func TestScanSignals(t *testing.T) {
	tokenWords := int((core.Cfg.TokenBits + 63) / 64)
	baseIdx := core.Cfg.TokenIndex

	// Create two Values with identical token regions → XOR = all zeros.
	a := &Value{}
	b := &Value{}
	a[core.Cfg.ValueID] = 100
	b[core.Cfg.ValueID] = 200

	for w := 0; w < tokenWords; w++ {
		idx := baseIdx + w
		if idx >= Words {
			break
		}
		a[idx] = 0xDEADBEEFCAFEBABE
		b[idx] = 0xDEADBEEFCAFEBABE
	}

	signals := ScanSignals(a, b, tokenWords, baseIdx)
	if len(signals) == 0 {
		t.Fatal("expected at least one signal from identical Values")
	}

	// The longest signal should be a cancel (XOR→zero run) spanning
	// the entire token region.
	local, exchange := SplitSignals(signals)
	if len(local) == 0 {
		t.Fatal("expected at least one local signal")
	}

	foundCancel := false
	for _, sig := range local {
		if sig.Kind == SignalCancel {
			foundCancel = true
			expectedBits := tokenWords * 64
			if sig.Length != expectedBits {
				t.Errorf("cancel expected length %d, got %d", expectedBits, sig.Length)
			}
		}
	}
	if !foundCancel {
		t.Error("expected a cancel signal from identical Values")
	}

	// Also expect merge (AND→one run) for the same reason.
	foundMerge := false
	for _, sig := range local {
		if sig.Kind == SignalMerge {
			foundMerge = true
		}
	}
	if !foundMerge {
		t.Error("expected a merge signal from identical Values")
	}

	_ = exchange // exchange may be empty when only one pair exists
}

func TestScanSignalsDifferent(t *testing.T) {
	tokenWords := int((core.Cfg.TokenBits + 63) / 64)
	baseIdx := core.Cfg.TokenIndex

	// Two completely different Values → XOR = all ones, AND = all zeros.
	a := &Value{}
	b := &Value{}
	a[core.Cfg.ValueID] = 300
	b[core.Cfg.ValueID] = 400

	for w := 0; w < tokenWords; w++ {
		idx := baseIdx + w
		if idx >= Words {
			break
		}
		a[idx] = 0xAAAAAAAAAAAAAAAA
		b[idx] = 0x5555555555555555
	}

	signals := ScanSignals(a, b, tokenWords, baseIdx)
	local, _ := SplitSignals(signals)

	// XOR of complementary patterns = all ones → zero run should be 0.
	for _, sig := range local {
		if sig.Kind == SignalCancel && sig.Length == tokenWords*64 {
			t.Error("should NOT have full-width cancel for complementary Values")
		}
	}
}

func TestSplitSignals(t *testing.T) {
	signals := []Signal{
		{Kind: SignalCancel, Length: 100, SourceA: 1, SourceB: 2},
		{Kind: SignalCancel, Length: 50, SourceA: 1, SourceB: 3},
		{Kind: SignalMerge, Length: 80, SourceA: 1, SourceB: 2},
		{Kind: SignalMerge, Length: 20, SourceA: 4, SourceB: 5},
	}
	sortSignals(signals)
	local, exchange := SplitSignals(signals)

	if len(local) != 2 {
		t.Fatalf("expected 2 local signals, got %d", len(local))
	}
	if local[0].Length != 100 {
		t.Errorf("first local should be longest cancel (100), got %d", local[0].Length)
	}
	if len(exchange) != 2 {
		t.Fatalf("expected 2 exchange signals, got %d", len(exchange))
	}
}

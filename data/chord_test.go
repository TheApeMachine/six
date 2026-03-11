package data

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitize_ZerosHighBits(t *testing.T) {
	t.Parallel()

	var c Chord
	// Pollute bits above 256
	c[4] = 0xFFFFFFFFFFFFFFFF
	c[5] = 0xFFFFFFFFFFFFFFFF
	c[6] = 0xFFFFFFFFFFFFFFFF
	c[7] = 0xFFFFFFFFFFFFFFFF

	c.Sanitize()

	// Word 4 should only have bit 0 (bit 256 = delimiter face)
	require.Equal(t, uint64(1), c[4])
	require.Equal(t, uint64(0), c[5])
	require.Equal(t, uint64(0), c[6])
	require.Equal(t, uint64(0), c[7])
}

func TestSanitize_PreservesLowBits(t *testing.T) {
	t.Parallel()

	var c Chord
	c[0] = 0xDEADBEEF
	c[1] = 0xCAFEBABE
	c[2] = 0x12345678
	c[3] = 0xABCDEF01
	c[4] = 0x03 // bits 256 and 257 — only 256 should survive

	c.Sanitize()

	require.Equal(t, uint64(0xDEADBEEF), c[0])
	require.Equal(t, uint64(0xCAFEBABE), c[1])
	require.Equal(t, uint64(0x12345678), c[2])
	require.Equal(t, uint64(0xABCDEF01), c[3])
	require.Equal(t, uint64(1), c[4]) // only bit 256 survives
}

func TestChordOR_Sanitized(t *testing.T) {
	t.Parallel()

	var a, b Chord
	a[0] = 0xFF
	a[5] = 0x01 // dirty high bit — should not survive OR

	b[0] = 0xFF00
	b[6] = 0x01 // dirty high bit

	result := ChordOR(&a, &b)

	require.Equal(t, uint64(0xFFFF), result[0])
	require.Equal(t, uint64(0), result[5], "high bits should be sanitized after OR")
	require.Equal(t, uint64(0), result[6], "high bits should be sanitized after OR")
}

func TestBaseChord_AllBitsWithinLogicalWidth(t *testing.T) {
	t.Parallel()

	const logicalBits = 257

	for b := 0; b < 256; b++ {
		chord := BaseChord(byte(b))

		// No bits should be set above bit 256
		for i := logicalBits; i < 512; i++ {
			word := i / 64
			bit := i % 64
			if chord[word]&(1<<uint(bit)) != 0 {
				t.Fatalf("BaseChord(%d) has bit %d set (above logical width %d)", b, i, logicalBits)
			}
		}

		// Should have some active bits
		require.Greater(t, chord.ActiveCount(), 0, "BaseChord(%d) should have active bits", b)
	}
}

func TestBaseChord_AllValuesUnique(t *testing.T) {
	t.Parallel()

	chords := make(map[Chord]byte)

	for b := 0; b < 256; b++ {
		chord := BaseChord(byte(b))
		if prev, exists := chords[chord]; exists {
			t.Fatalf("BaseChord(%d) collides with BaseChord(%d)", b, prev)
		}
		chords[chord] = byte(b)
	}
}

func TestRollLeft_StaysWithinLogicalWidth(t *testing.T) {
	t.Parallel()

	const logicalBits = 257

	chord := BaseChord('A')
	rolled := chord.RollLeft(42)

	for i := logicalBits; i < 512; i++ {
		word := i / 64
		bit := i % 64
		if rolled[word]&(1<<uint(bit)) != 0 {
			t.Fatalf("RollLeft produced bit %d (above logical width %d)", i, logicalBits)
		}
	}

	// Active count should be preserved
	require.Equal(t, chord.ActiveCount(), rolled.ActiveCount())
}

func TestBestByte_DecodesBoundChord(t *testing.T) {
	t.Parallel()

	base := BaseChord('k')
	bound := base.BindPosition(11)

	require.Equal(t, byte('k'), bound.BestByte())
}

func TestRotationSeed_UsesStructureNotDensityOnly(t *testing.T) {
	t.Parallel()

	var left Chord
	left.Set(3)
	left.Set(17)
	left.Set(41)

	var right Chord
	right.Set(5)
	right.Set(19)
	right.Set(43)

	require.Equal(t, left.ActiveCount(), right.ActiveCount())

	aLeft, bLeft := left.RotationSeed()
	aRight, bRight := right.RotationSeed()

	require.NotEqual(t, [2]uint16{aLeft, bLeft}, [2]uint16{aRight, bRight})
}

func TestMaskChord_UsesControlFace(t *testing.T) {
	t.Parallel()

	mask := MaskChord()

	require.Equal(t, 1, mask.ActiveCount())
	require.True(t, mask.Has(256))
}

func TestBindGeometry_SuperposesCarrier(t *testing.T) {
	t.Parallel()

	base := BaseChord('x')
	carrier := BaseChord('!')
	bound := base.BindGeometry(7, &carrier)

	require.Greater(t, ChordSimilarity(&bound, &base), 0)
	require.Greater(t, ChordSimilarity(&bound, &carrier), 0)
	require.Greater(t, bound.ActiveCount(), base.ActiveCount())
}

func BenchmarkChordRotationSeed(b *testing.B) {
	chord := BaseChord('x')
	chord = chord.BindGeometry(17, nil)

	for b.Loop() {
		_, _ = chord.RotationSeed()
	}
}

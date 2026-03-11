package resonance

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/data"
)

func chordBytes(text string) []data.Chord {
	out := make([]data.Chord, 0, len(text))
	for i := range len(text) {
		out = append(out, data.BaseChord(text[i]))
	}
	return out
}

func TestSequenceFieldTopFollowersAndPairScore(t *testing.T) {
	t.Parallel()

	field := NewSequenceField([][]data.Chord{
		chordBytes("ab"),
		chordBytes("ac"),
		chordBytes("ab"),
	})

	followers := field.TopFollowers(data.BaseChord('a'), 2)
	require.Len(t, followers, 2)
	require.Equal(t, byte('b'), followers[0].Chord.Byte())
	require.Greater(t, field.PairScore(data.BaseChord('a'), data.BaseChord('b')), 0.0)
	require.Zero(t, field.PairScore(data.BaseChord('b'), data.BaseChord('a')))
}

func TestSequenceFieldTopBridges(t *testing.T) {
	t.Parallel()

	field := NewSequenceField([][]data.Chord{
		chordBytes("axr"),
		chordBytes("axr"),
		chordBytes("ayr"),
	})

	bridges := field.TopBridges(data.BaseChord('a'), data.BaseChord('r'), 2)
	require.Len(t, bridges, 2)
	require.Equal(t, byte('x'), bridges[0].Chord.Byte())
	require.Greater(t, field.BridgeScore(data.BaseChord('a'), data.BaseChord('x'), data.BaseChord('r')), field.BridgeScore(data.BaseChord('a'), data.BaseChord('y'), data.BaseChord('r')))
}

func TestSequenceFieldTopMiddlesAndTripletScore(t *testing.T) {
	t.Parallel()

	field := NewSequenceField([][]data.Chord{
		chordBytes("axr"),
		chordBytes("axr"),
		chordBytes("axr"),
		chordBytes("ayr"),
	})

	middles := field.TopMiddles(data.BaseChord('a'), data.BaseChord('r'), 2)
	require.Len(t, middles, 2)
	require.Equal(t, byte('x'), middles[0].Chord.Byte())
	require.Greater(t, field.TripletScore(data.BaseChord('a'), data.BaseChord('x'), data.BaseChord('r')), field.TripletScore(data.BaseChord('a'), data.BaseChord('y'), data.BaseChord('r')))
	require.Zero(t, field.TripletScore(data.BaseChord('a'), data.BaseChord('z'), data.BaseChord('r')))
}

func BenchmarkSequenceFieldTopMiddles(b *testing.B) {
	corpus := make([][]data.Chord, 0, 1024)
	for range 512 {
		corpus = append(corpus, chordBytes("axr"))
		corpus = append(corpus, chordBytes("ayr"))
	}

	field := NewSequenceField(corpus)
	left := data.BaseChord('a')
	right := data.BaseChord('r')

	b.ResetTimer()
	for b.Loop() {
		_ = field.TopMiddles(left, right, 8)
	}
}

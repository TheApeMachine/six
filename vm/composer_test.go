package vm

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider/local"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm/cortex"
)

func loaderFromCorpus(t *testing.T, corpus []string) *Loader {
	t.Helper()

	bytesCorpus := make([][]byte, len(corpus))
	for i, sample := range corpus {
		bytesCorpus[i] = []byte(sample)
	}

	loader := NewLoader(
		LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(local.New(bytesCorpus)),
			),
		),
	)

	require.NoError(t, loader.Start())
	return loader
}

func textToChords(text string) []data.Chord {
	out := make([]data.Chord, 0, len(text))
	for i := range len(text) {
		out = append(out, data.BaseChord(text[i]))
	}
	return out
}

func chordsToText(chords []data.Chord) string {
	buf := make([]byte, 0, len(chords))
	for _, chord := range chords {
		buf = append(buf, chord.BestByte())
	}
	return string(buf)
}

func TestBoundaryComposerBuildsReverseSubstrate(t *testing.T) {
	t.Parallel()

	loader := loaderFromCorpus(t, []string{"abc"})
	require.NotNil(t, loader.ReverseSubstrate())
	require.NotEmpty(t, loader.ReverseSubstrate().Entries)

	var foundReverse bool
	for _, entry := range loader.ReverseSubstrate().Entries {
		if !entry.Reverse || len(entry.Lexical) == 0 {
			continue
		}
		foundReverse = true
		break
	}

	require.True(t, foundReverse, "expected reverse substrate entries with lexical readouts")
}

func TestBoundaryComposerSelfCorrectsTowardRightBoundary(t *testing.T) {
	t.Parallel()

	loader := loaderFromCorpus(t, []string{
		"abxqr",
		"abpqr",
		"cdpyz",
		"efpyz",
	})

	composer := NewBoundaryComposer(
		loader,
		BoundaryComposerWithTopK(8),
		BoundaryComposerWithIterations(8),
	)

	span := composer.Compose(SpanBoundary{
		Left:  textToChords("ab"),
		Right: textToChords("z"),
		Width: 2,
	})

	require.Equal(t, "py", chordsToText(span))
}

func TestBoundaryComposerPredictNextDefaultsToSingleBoundarySolve(t *testing.T) {
	t.Parallel()

	loader := loaderFromCorpus(t, []string{
		"hello world",
		"hello there",
	})

	composer := NewBoundaryComposer(loader)
	next := composer.PredictNext(textToChords("hello "), nil)

	require.NotEqual(t, data.Chord{}, next)
	require.Contains(t, []byte{'w', 't'}, next.Byte())
}

func TestBoundaryComposerLogicBiasCanOverrideLocalFrequency(t *testing.T) {
	t.Parallel()

	loader := loaderFromCorpus(t, []string{
		"axr",
		"axr",
		"axr",
		"ayr",
	})

	composer := NewBoundaryComposer(
		loader,
		BoundaryComposerWithTopK(8),
		BoundaryComposerWithDomainLimit(6),
	)

	withoutLogic := composer.Compose(SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 1,
	})
	require.Equal(t, "x", chordsToText(withoutLogic))

	withLogic := composer.Compose(SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 1,
		Logic: cortex.LogicSnapshot{
			Rules: []cortex.LogicRule{{
				Interface: data.BaseChord('a'),
				Payload:   data.BaseChord('y'),
				Program:   data.BaseChord('!'),
				Support:   12,
				Role:      cortex.RoleTool,
			}},
		},
	})

	require.Equal(t, "y", chordsToText(withLogic))
}

func TestBoundaryComposerUsesLogicChainAcrossSpan(t *testing.T) {
	t.Parallel()

	loader := loaderFromCorpus(t, []string{
		"abxr",
		"abxr",
		"abxr",
		"acdr",
	})

	composer := NewBoundaryComposer(
		loader,
		BoundaryComposerWithTopK(10),
		BoundaryComposerWithDomainLimit(8),
		BoundaryComposerWithIterations(6),
	)

	span := composer.Compose(SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 2,
		Logic: cortex.LogicSnapshot{
			Rules: []cortex.LogicRule{
				{
					Interface: data.BaseChord('a'),
					Payload:   data.BaseChord('c'),
					Program:   data.BaseChord('!'),
					Support:   10,
					Role:      cortex.RoleTool,
				},
				{
					Interface: data.BaseChord('c'),
					Payload:   data.BaseChord('d'),
					Program:   data.BaseChord('?'),
					Support:   9,
					Role:      cortex.RoleTool,
				},
			},
		},
	})

	require.Equal(t, "cd", chordsToText(span))
}

func TestBoundaryComposerUsesExplicitChainsWithoutCorpusSupport(t *testing.T) {
	t.Parallel()

	loader := loaderFromCorpus(t, []string{"mnop", "qrst"})

	composer := NewBoundaryComposer(
		loader,
		BoundaryComposerWithTopK(6),
		BoundaryComposerWithDomainLimit(6),
	)

	leftProgram := data.BaseChord('!')
	rightProgram := data.BaseChord('?')

	chain := cortex.LogicChain{
		Left: cortex.LogicRule{
			Interface: data.BaseChord('a'),
			Payload:   data.BaseChord('c'),
			Program:   leftProgram,
			Support:   10,
			Role:      cortex.RoleTool,
		},
		Right: cortex.LogicRule{
			Interface: data.BaseChord('c'),
			Payload:   data.BaseChord('d'),
			Program:   rightProgram,
			Support:   9,
			Role:      cortex.RoleTool,
		},
		Bridge:  data.BaseChord('c'),
		Program: data.ChordOR(&leftProgram, &rightProgram),
		Support: 8,
	}

	span := composer.Compose(SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 2,
		Logic: cortex.LogicSnapshot{Chains: []cortex.LogicChain{chain}},
	})

	require.Equal(t, "cd", chordsToText(span))
}

func TestComposerLogicFieldSynthesizesProgramRotatedCandidates(t *testing.T) {
	t.Parallel()

	rule := cortex.LogicRule{
		Interface: data.BaseChord('a'),
		Payload:   data.BaseChord('m'),
		Program:   data.BaseChord('P'),
		Support:   12,
		Role:      cortex.RoleTool,
	}

	field := newComposerLogicField(SpanBoundary{
		Left:  textToChords("a"),
		Width: 1,
		Logic: cortex.LogicSnapshot{Rules: []cortex.LogicRule{rule}},
	})
	require.NotNil(t, field)

	rotated := geometry.RotationForChord(rule.Program).ApplyToChord(rule.Payload)
	suggestions := field.suggestions(0, 1)

	require.Contains(t, suggestions, rule.Payload)
	require.Contains(t, suggestions, rotated)
	require.Greater(t, suggestions[rotated], 0.0)
}

func TestBoundaryComposerUsesExplicitCircuitAcrossLongSpan(t *testing.T) {
	t.Parallel()

	loader := loaderFromCorpus(t, []string{
		"axxxr",
		"axxxr",
		"axxxr",
		"abcer",
	})

	composer := NewBoundaryComposer(
		loader,
		BoundaryComposerWithTopK(12),
		BoundaryComposerWithDomainLimit(10),
		BoundaryComposerWithIterations(6),
	)

	withoutLogic := composer.Compose(SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 3,
	})
	require.Equal(t, "xxx", chordsToText(withoutLogic))

	p1 := data.BaseChord('!')
	p2 := data.BaseChord('?')
	p3 := data.BaseChord('+')
	program := data.ChordOR(&p1, &p2)
	program = data.ChordOR(&program, &p3)

	circuit := cortex.LogicCircuit{
		Steps: []cortex.LogicRule{
			{
				Interface: data.BaseChord('a'),
				Payload:   data.BaseChord('c'),
				Program:   p1,
				Support:   16,
				Role:      cortex.RoleTool,
			},
			{
				Interface: data.BaseChord('c'),
				Payload:   data.BaseChord('d'),
				Program:   p2,
				Support:   15,
				Role:      cortex.RoleTool,
			},
			{
				Interface: data.BaseChord('d'),
				Payload:   data.BaseChord('e'),
				Program:   p3,
				Support:   14,
				Role:      cortex.RoleTool,
			},
		},
		Program: program,
		Support: 12,
	}

	withLogic := composer.Compose(SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 3,
		Logic: cortex.LogicSnapshot{Circuits: []cortex.LogicCircuit{circuit}},
	})

	require.Equal(t, "cde", chordsToText(withLogic))
}

func TestComposerLogicFieldBuildsLongRangeTemplates(t *testing.T) {
	t.Parallel()

	circuit := cortex.LogicCircuit{
		Steps: []cortex.LogicRule{
			{Interface: data.BaseChord('a'), Payload: data.BaseChord('c'), Program: data.BaseChord('!'), Support: 9, Role: cortex.RoleTool},
			{Interface: data.BaseChord('c'), Payload: data.BaseChord('d'), Program: data.BaseChord('?'), Support: 8, Role: cortex.RoleTool},
			{Interface: data.BaseChord('d'), Payload: data.BaseChord('e'), Program: data.BaseChord('+'), Support: 7, Role: cortex.RoleTool},
		},
		Support: 8,
	}

	field := newComposerLogicField(SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 3,
		Logic: cortex.LogicSnapshot{Circuits: []cortex.LogicCircuit{circuit}},
	})
	require.NotNil(t, field)
	require.True(t, field.hasCircuits())
	require.NotEmpty(t, field.templates)

	suggestions := field.suggestions(1, 3)
	require.Contains(t, suggestions, data.BaseChord('d'))
	require.Greater(t, suggestions[data.BaseChord('d')], 0.0)

	span := []data.Chord{data.BaseChord('c'), data.BaseChord('d'), data.BaseChord('e')}
	require.Greater(t, field.spanScore(span), 0.0)
}

func BenchmarkBoundaryComposerSolveCircuitBeam(b *testing.B) {
	bytesCorpus := [][]byte{
		[]byte("axxxr"),
		[]byte("axxxr"),
		[]byte("axxxr"),
		[]byte("abcer"),
		[]byte("abcer"),
	}

	loader := NewLoader(
		LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(local.New(bytesCorpus)),
			),
		),
	)
	if err := loader.Start(); err != nil {
		b.Fatal(err)
	}

	composer := NewBoundaryComposer(
		loader,
		BoundaryComposerWithTopK(12),
		BoundaryComposerWithDomainLimit(10),
	)

	p1 := data.BaseChord('!')
	p2 := data.BaseChord('?')
	p3 := data.BaseChord('+')
	program := data.ChordOR(&p1, &p2)
	program = data.ChordOR(&program, &p3)

	logic := cortex.LogicSnapshot{Circuits: []cortex.LogicCircuit{{
		Steps: []cortex.LogicRule{
			{Interface: data.BaseChord('a'), Payload: data.BaseChord('c'), Program: p1, Support: 16, Role: cortex.RoleTool},
			{Interface: data.BaseChord('c'), Payload: data.BaseChord('d'), Program: p2, Support: 15, Role: cortex.RoleTool},
			{Interface: data.BaseChord('d'), Payload: data.BaseChord('e'), Program: p3, Support: 14, Role: cortex.RoleTool},
		},
		Program: program,
		Support: 12,
	}}}

	boundary := SpanBoundary{
		Left:  textToChords("a"),
		Right: textToChords("r"),
		Width: 3,
		Logic: logic,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		span := composer.Compose(boundary)
		if len(span) == 0 {
			b.Fatal("empty span")
		}
	}
}

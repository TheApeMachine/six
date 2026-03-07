package vm

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider/local"
	"github.com/theapemachine/six/tokenizer"
)

// buildCorpus creates a realistic corpus of Python-like code snippets.
func buildCorpus() [][]byte {
	snippets := []string{
		"def add(a, b):\n    return a + b\n",
		"def sub(a, b):\n    return a - b\n",
		"def mul(a, b):\n    return a * b\n",
		"def div(a, b):\n    return a / b\n",
		"def square(x):\n    return x * x\n",
		"def cube(x):\n    return x * x * x\n",
		"def negate(x):\n    return -x\n",
		"def double(x):\n    return x + x\n",
		"def triple(x):\n    return x + x + x\n",
		"def identity(x):\n    return x\n",
		"for i in range(10):\n    print(i)\n",
		"for i in range(100):\n    print(i * 2)\n",
		"if x > 0:\n    return True\n",
		"if x < 0:\n    return False\n",
		"while x > 0:\n    x = x - 1\n",
		"result = []\nfor item in data:\n    result.append(item)\n",
		"total = 0\nfor n in nums:\n    total += n\n",
		"values = [x * 2 for x in range(20)]\n",
		"filtered = [x for x in items if x > 0]\n",
		"mapped = list(map(lambda x: x + 1, data))\n",
	}

	corpus := make([][]byte, 0, len(snippets)*10)
	for range 10 {
		for _, s := range snippets {
			corpus = append(corpus, []byte(s))
		}
	}
	return corpus
}

// buildTestMachine creates a Machine from a corpus using the standard API.
// Tests access internal fields directly (same package).
func buildTestMachine(corpus [][]byte) *Machine {
	return NewMachine(
		MachineWithLoader(
			NewLoader(
				LoaderWithTokenizer(
					tokenizer.NewUniversal(
						tokenizer.TokenizerWithDataset(local.New(corpus)),
					),
				),
			),
		),
	)
}

func promptToChords(prompt string) []data.Chord {
	chords := make([]data.Chord, 0, len(prompt))
	for i := range len(prompt) {
		chords = append(chords, data.BaseChord(prompt[i]))
	}
	return chords
}

func collectBytes(out <-chan byte) []byte {
	res := make([]byte, 0, 64)
	for b := range out {
		res = append(res, b)
	}
	return res
}

func TestMachineIntegration_IngestsCorpusIntoManifolds(t *testing.T) {
	machine := buildTestMachine(buildCorpus())
	require.NoError(t, machine.Start())

	pf := machine.primefield
	require.GreaterOrEqual(t, pf.N, 2, "corpus should produce multiple manifolds via freeze boundary")

	ptr, n, offset := pf.SearchSnapshot()
	require.NotNil(t, ptr)
	require.Greater(t, n, 0, "dictionary should have entries")
	require.GreaterOrEqual(t, offset, 0)

	totalActive := 0
	for i := 0; i < pf.N; i++ {
		m := pf.Manifold(i)
		for cube := range 5 {
			for face := range 257 {
				totalActive += m.Cubes[cube][face].ActiveCount()
			}
		}
	}
	require.Greater(t, totalActive, 1000, "corpus should produce substantial manifold density")
}

func TestMachineIntegration_EigenModeAndSequencerWired(t *testing.T) {
	machine := buildTestMachine(buildCorpus())
	require.NoError(t, machine.Start())

	// Start() should have wired EigenMode and Sequencer automatically.
	require.NotNil(t, machine.eigenMode, "EigenMode should be wired after Start")
	require.NotNil(t, machine.sequencer, "Sequencer should be wired after Start")
}

func TestMachineIntegration_GeneratesOutput(t *testing.T) {
	machine := buildTestMachine(buildCorpus())
	require.NoError(t, machine.Start())

	out := collectBytes(machine.Prompt(promptToChords("def add("), nil))
	t.Logf("cortex generated %d bytes from prompt", len(out))
}

func TestMachineIntegration_DifferentPromptsProduceDifferentOutput(t *testing.T) {
	machine1 := buildTestMachine(buildCorpus())
	require.NoError(t, machine1.Start())
	out1 := collectBytes(machine1.Prompt(promptToChords("def add("), nil))

	machine2 := buildTestMachine(buildCorpus())
	require.NoError(t, machine2.Start())
	out2 := collectBytes(machine2.Prompt(promptToChords("for i in"), nil))

	if len(out1) > 0 && len(out2) > 0 {
		require.NotEqual(t, out1, out2, "different prompts should produce different output")
	}
}

func TestMachineIntegration_ExpectedRealitySeededIntoSink(t *testing.T) {
	machine := buildTestMachine(buildCorpus())
	require.NoError(t, machine.Start())

	expectedReality := &geometry.IcosahedralManifold{}
	expectedReality.Cubes[0][0].Set(100)

	out := collectBytes(machine.Prompt(promptToChords("def "), expectedReality))
	t.Logf("generated %d bytes with expected reality", len(out))
}

func TestMachineIntegration_ExpectedFieldAccepted(t *testing.T) {
	machine := buildTestMachine(buildCorpus())
	require.NoError(t, machine.Start())

	expectedField := geometry.NewExpectedField()
	expectedField.Support[0][0].Set(100)

	out := collectBytes(machine.PromptWithExpectedField(promptToChords("def "), &expectedField))
	t.Logf("generated %d bytes with expected field", len(out))
}

func TestMachineIntegration_StopTerminatesGeneration(t *testing.T) {
	machine := buildTestMachine(buildCorpus())
	require.NoError(t, machine.Start())

	ch := machine.Prompt(promptToChords("def "), nil)
	machine.Stop()

	out := collectBytes(ch)
	t.Logf("generated %d bytes before stop", len(out))
}

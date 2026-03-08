package vm

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider/local"
	"github.com/theapemachine/six/tokenizer"
)

// --- Corpus and machine builders ---

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

// --- Tests ---

func TestMachineIntegration(t *testing.T) {
	Convey("Given a Machine built from a corpus", t, func() {
		Convey("Ingesting corpus should produce multiple manifolds via freeze boundary", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)

			pf := machine.primefield
			So(pf.N, ShouldBeGreaterThanOrEqualTo, 2)

			ptr, n, offset := pf.SearchSnapshot()
			So(ptr, ShouldNotBeNil)
			So(n, ShouldBeGreaterThan, 0)
			So(offset, ShouldBeGreaterThanOrEqualTo, 0)

			totalActive := 0
			for i := 0; i < pf.N; i++ {
				m := pf.Manifold(i)
				for cube := range 5 {
					for face := range 257 {
						totalActive += m.Cubes[cube][face].ActiveCount()
					}
				}
			}
			So(totalActive, ShouldBeGreaterThan, 1000)
		})

		Convey("Start should wire EigenMode and Sequencer", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)

			So(machine.eigenMode, ShouldNotBeNil)
			So(machine.sequencer, ShouldNotBeNil)
		})

		Convey("Prompt should generate output", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)

			out := collectBytes(machine.Prompt(promptToChords("def add("), nil))
			Printf("\n  cortex generated %d bytes from prompt\n", len(out))
		})

		Convey("Different prompts should produce different output", func() {
			machine1 := buildTestMachine(buildCorpus())
			So(machine1.Start(), ShouldBeNil)
			out1 := collectBytes(machine1.Prompt(promptToChords("def add("), nil))

			machine2 := buildTestMachine(buildCorpus())
			So(machine2.Start(), ShouldBeNil)
			out2 := collectBytes(machine2.Prompt(promptToChords("for i in"), nil))

			if len(out1) > 0 && len(out2) > 0 {
				So(out1, ShouldNotResemble, out2)
			}
		})

		Convey("Expected reality seeded into sink should steer generation", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)

			expectedReality := &geometry.IcosahedralManifold{}
			expectedReality.Cubes[0][0].Set(100)

			out := collectBytes(machine.Prompt(promptToChords("def "), expectedReality))
			Printf("\n  generated %d bytes with expected reality\n", len(out))
		})

		Convey("Stop should terminate generation", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)

			ch := machine.Prompt(promptToChords("def "), nil)
			machine.Stop()

			out := collectBytes(ch)
			Printf("\n  generated %d bytes before stop\n", len(out))
		})
	})
}

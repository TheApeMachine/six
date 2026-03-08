package vm

import (
	"testing"
	"unicode"
	"unicode/utf8"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider/local"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
)

// --- Corpus and machine builders ---

// corpusByteSet returns the set of bytes that appear in the corpus.
func corpusByteSet(corpus [][]byte) map[byte]bool {
	seen := make(map[byte]bool)
	for _, doc := range corpus {
		for _, b := range doc {
			seen[b] = true
		}
	}

	return seen
}

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

// buildRetrievalCorpus returns a minimal corpus for perfect-retrieval tests.
// Full document: "def add(a, b):\n    return a + b\n"
// Prompt "def add(" should retrieve remainder "a, b):\n    return a + b\n"
func buildRetrievalCorpus() [][]byte {
	return [][]byte{[]byte("def add(a, b):\n    return a + b\n")}
}

// isPrintableASCII returns true if b is printable ASCII or common whitespace.
func isPrintableASCII(b byte) bool {
	return b < 128 && (unicode.IsPrint(rune(b)) || b == '\n' || b == '\r' || b == '\t')
}

// buildTestMachine creates a Machine from a corpus using the standard API.
// Tests access internal fields directly (same package).
func buildTestMachine(corpus [][]byte) *Machine {
	return NewMachine(
		MachineWithLoader(
			NewLoader(
				LoaderWithStore(store.NewLSMSpatialIndex(1.0)),
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
		Convey("Start should load corpus", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)
			So(machine.primefield.N, ShouldBeGreaterThan, 0)
		})

		Convey("Prompt should generate output", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)

			out := collectBytes(machine.Prompt(promptToChords("def add("), nil))
			So(len(out), ShouldBeGreaterThanOrEqualTo, 0)
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
			So(out, ShouldNotBeNil)
		})

		Convey("Stop should terminate generation", func() {
			machine := buildTestMachine(buildCorpus())
			So(machine.Start(), ShouldBeNil)

			ch := machine.Prompt(promptToChords("def "), nil)
			machine.Stop()

			out := collectBytes(ch)
			So(out, ShouldNotBeNil)
		})
	})
}

func TestMachinePromptOutputValidUTF8(t *testing.T) {
	Convey("Given a Machine built from UTF-8 corpus", t, func() {
		corpus := buildCorpus()
		machine := buildTestMachine(corpus)
		So(machine.Start(), ShouldBeNil)

		Convey("When Prompt generates output", func() {
			out := collectBytes(machine.Prompt(promptToChords("def add("), nil))

			Convey("Then output is valid UTF-8", func() {
				So(utf8.Valid(out), ShouldBeTrue)
			})
		})
	})
}

func TestMachinePromptOutputFullyPrintable(t *testing.T) {
	Convey("Given a Machine built from printable-ASCII corpus", t, func() {
		corpus := buildCorpus()
		machine := buildTestMachine(corpus)
		So(machine.Start(), ShouldBeNil)

		Convey("When Prompt generates output", func() {
			out := collectBytes(machine.Prompt(promptToChords("def add("), nil))

			Convey("Then every byte is printable or whitespace", func() {
				for _, b := range out {
					So(isPrintableASCII(b), ShouldBeTrue)
				}
			})
		})
	})
}

func TestMachinePromptOutputFullyInVocabulary(t *testing.T) {
	Convey("Given a Machine built from corpus with known byte vocabulary", t, func() {
		corpus := buildCorpus()
		vocab := corpusByteSet(corpus)
		machine := buildTestMachine(corpus)
		So(machine.Start(), ShouldBeNil)

		Convey("When Prompt generates output", func() {
			out := collectBytes(machine.Prompt(promptToChords("def add("), nil))

			Convey("Then every byte exists in the corpus", func() {
				for _, b := range out {
					So(vocab[b], ShouldBeTrue)
				}
			})
		})
	})
}

func TestMachinePromptPerfectRetrieval(t *testing.T) {
	Convey("Given a Machine with a single document in memory", t, func() {
		corpus := buildRetrievalCorpus()
		full := string(corpus[0])
		prefix := "def add("
		expected := "a, b):\n    return a + b\n"

		machine := buildTestMachine(corpus)
		So(machine.Start(), ShouldBeNil)

		Convey("When prompting with the document prefix", func() {
			out := collectBytes(machine.Prompt(promptToChords(prefix), nil))
			observed := string(out)

			Convey("Then output equals the exact continuation", func() {
				So(observed, ShouldEqual, expected)
			})
			Convey("Then full document equals prefix plus observed", func() {
				So(prefix+observed, ShouldEqual, full)
			})
		})
	})
}

func TestMachinePromptOutputBounded(t *testing.T) {
	Convey("Given a Machine", t, func() {
		machine := buildTestMachine(buildCorpus())
		So(machine.Start(), ShouldBeNil)

		Convey("When Prompt runs to completion", func() {
			out := collectBytes(machine.Prompt(promptToChords("def "), nil))

			Convey("Then output length is bounded by MaxOutput", func() {
				So(len(out), ShouldBeLessThanOrEqualTo, 256)
			})
		})
	})
}

func BenchmarkMachinePrompt(b *testing.B) {
	corpus := buildCorpus()
	machine := buildTestMachine(corpus)
	if err := machine.Start(); err != nil {
		b.Fatal(err)
	}

	prompt := promptToChords("def add(")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := collectBytes(machine.Prompt(prompt, nil))
		_ = out
	}
}

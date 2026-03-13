package vm

import (
	"unicode"

	"github.com/theapemachine/six/pkg/data"
)

// --- Corpus and machine builders ---

// corpusByteSet returns the set of bytes that appear in the corpus.
func corpusByteSet(corpus [][]byte) map[byte]bool {
	seen := make(map[byte]bool)
	for _, doc := range corpus {
		for _, byteVal := range doc {
			seen[byteVal] = true
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
		for _, snippet := range snippets {
			corpus = append(corpus, []byte(snippet))
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

// isPrintableASCII returns true if charByte is printable ASCII or common whitespace.
func isPrintableASCII(charByte byte) bool {
	return charByte < 128 && (unicode.IsPrint(rune(charByte)) || charByte == '\n' || charByte == '\r' || charByte == '\t')
}

// buildTestMachine creates a Machine from a corpus using the standard API.
// Tests access internal fields directly (same package).
func buildTestMachine(corpus [][]byte) *Machine {
	return NewMachine()
}

func promptToChords(prompt string) []data.Chord {
	chords := make([]data.Chord, 0, len(prompt))
	for idx := range len(prompt) {
		chords = append(chords, data.BaseChord(prompt[idx]))
	}
	return chords
}

func collectBytes(out <-chan byte) []byte {
	res := make([]byte, 0, 64)
	for byteVal := range out {
		res = append(res, byteVal)
	}
	return res
}

// --- Tests ---

// -- tests commented out while kernel API is under construction --
/*
func TestMachineIntegration(t *testing.T) {
	Convey("Given a Machine built from a corpus", t, func() {
		Convey("Start should load corpus", func() {
			machine := buildTestMachine(buildCorpus())
			defer machine.Stop()
			So(machine.Start(), ShouldBeNil)
		})
	})
}
*/

/*
func TestMachinePromptOutputValidUTF8(t *testing.T) {
	Convey("Given a Machine built from UTF-8 corpus", t, func() {
		corpus := buildCorpus()
		machine := buildTestMachine(corpus)
		So(machine.Start(), ShouldBeNil)

		Convey("When Prompt generates output", func() {
			out := <-machine.Prompt(promptToChords("def add("))

			Convey("Then output is valid UTF-8", func() {
				bytesOut := make([]byte, len(out))
				for i, c := range out {
					bytesOut[i] = byte(c.IntrinsicFace() % 256)
				}
				So(utf8.Valid(bytesOut), ShouldBeTrue)
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
			out := <-machine.Prompt(promptToChords("def add("))

			Convey("Then every byte is printable or whitespace", func() {
				for _, c := range out {
					b := byte(c.IntrinsicFace() % 256)
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
			out := <-machine.Prompt(promptToChords("def add("))

			Convey("Then every byte exists in the corpus", func() {
				for _, c := range out {
					b := byte(c.IntrinsicFace() % 256)
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
			out := <-machine.Prompt(promptToChords(prefix))
			bytesOut := make([]byte, len(out))
			for i, c := range out {
				bytesOut[i] = byte(c.IntrinsicFace() % 256)
			}
			observed := string(bytesOut)

			Convey("Then output equals the exact continuation", func() {
				So(observed, ShouldEqual, expected)
			})
			Convey("Then full document equals prefix plus observed", func() {
				So(prefix+observed, ShouldEqual, full)
			})
		})
	})
}
*/

/*
func TestMachinePromptOutputBounded(t *testing.T) {
	Convey("Given a Machine", t, func() {
		machine := buildTestMachine(buildCorpus())
		So(machine.Start(), ShouldBeNil)

		Convey("When Prompt runs to completion", func() {
			out := <-machine.Prompt(promptToChords("def "))

			Convey("Then output length is bounded by MaxOutput", func() {
				So(len(out), ShouldBeLessThanOrEqualTo, 256)
			})
		})
	})
}
*/

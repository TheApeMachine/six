package substrate

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/process/sequencer"
)

/*
tokenize uses the same Sequitur + BitwiseHealer pipeline as the tokenizer.
*/
func tokenize(raw []byte) [][]byte {
	seq := sequencer.NewSequitur()
	healer := sequencer.NewBitwiseHealer()
	var chunks [][]byte

	for pos, b := range raw {
		byteVal, isBoundary := seq.Analyze(uint32(pos), b)
		healer.Write(byteVal, isBoundary)
		if buf := healer.Heal(); buf != nil {
			chunks = append(chunks, buf...)
		}
	}

	if buf := healer.Flush(); buf != nil {
		chunks = append(chunks, buf...)
	}

	return chunks
}

/*
buildPaths converts raw chunks into values using BuildValue.
*/
func buildPaths(chunks [][]byte) ([]data.Value, error) {
	paths := make([]data.Value, len(chunks))

	var err error
	for i, chunk := range chunks {
		paths[i], err = data.BuildValue(chunk)
		if err != nil {
			return nil, err
		}
	}

	return paths, nil
}

func TestRecursiveFold(t *testing.T) {
	Convey("Given a graph with incoming data", t, func() {
		graph := NewGraphServer()
		_ = graph.Close()
	})
}

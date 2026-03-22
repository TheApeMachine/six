package integration

import (
	"testing"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
FuzzSequenceAccumulationNoPanic feeds arbitrary four-byte token runs through the
same accumulateSequence path used in integration tests. Panics or crashes are
reported as fuzz failures.

	go test ./test/integration -fuzz=FuzzSequenceAccumulationNoPanic -fuzztime=30s
*/
func FuzzSequenceAccumulationNoPanic(f *testing.F) {
	f.Add([]byte{42, 77, 210, 105})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			t.Skip()
		}

		tokens := data[:4]
		values := make([]*primitive.Value, len(tokens))

		for i, tok := range tokens {
			values[i] = primitive.BaseValue(tok)
		}

		_ = accumulateSequence(values)
	})
}

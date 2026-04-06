package markovtrie

import "github.com/theapemachine/six/pkg/core/algo/beam"

/*
CurrentStep returns the training step counter.
*/
func (store *Store) CurrentStep() int {
	if store == nil {
		return 0
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.currentStep
}

/*
Generate samples a continuation; temperature 0 follows the interpolated argmax path.
*/
func (store *Store) Generate(
	context string, label string, temperature float64, maxLength int,
) (string, error) {
	if store == nil {
		return "", nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	search := beam.NewSearch(
		store.ctx,
		[]string{context},
		3,
		maxLength,
		store.bpe.EndToken,
		"",
	)

	continuations := search.Run()

	if len(continuations) == 0 {
		return "", nil
	}

	return continuations[0].Sequence, nil
}

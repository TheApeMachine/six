package markovtrie

/*
ReplayOne samples one label, generates a candidate continuation, and reinserts
it only when it is confident and novel.
*/
func (store *Store) ReplayOne(temperature float64) *ReplayResult {
	if len(store.labels) == 0 {
		return nil
	}

	label := store.labels[store.random.Intn(len(store.labels))]
	sequence := store.Generate("", label, temperature, defaultReplayLength)
	if len(sequence) < 2 {
		return nil
	}

	scores := store.Classify(sequence)
	confidence := scores[label]
	if confidence <= defaultReplayThreshold {
		return nil
	}

	node := store.root
	novel := false
	for _, token := range store.Tokenize(sequence) {
		child := node.Children[token]
		if child == nil {
			novel = true
			break
		}

		node = child
	}

	if !novel {
		return nil
	}

	store.Insert(sequence, label)
	return &ReplayResult{
		Sequence:   sequence,
		Label:      label,
		Confidence: confidence,
	}
}

package markovtrie

/*
ReplayOne samples one label, generates a candidate continuation, and reinserts
it only when it is confident and novel.
*/
func (store *Store) ReplayOne(temperature float64) *ReplayResult {
	if store == nil || len(store.labels) == 0 {
		return nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	label := store.labels[store.random.Intn(len(store.labels))]
	sequence := store.generateBody("", label, temperature, defaultReplayLength)
	if len(sequence) < 2 {
		return nil
	}

	scores := store.classifyBody(sequence)
	confidence := scores[label]
	if confidence <= defaultReplayThreshold {
		return nil
	}

	node := store.root
	novel := false
	for _, token := range store.tokenizeUnlocked(sequence) {
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

	store.trainBody(sequence, label, 1)

	return &ReplayResult{
		Sequence:   sequence,
		Label:      label,
		Confidence: confidence,
	}
}

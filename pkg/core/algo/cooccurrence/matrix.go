package cooccurrence

/*
Matrix tracks word co-occurrence counts within a sliding window
and maintains insertion-ordered vocabulary.

Not safe for concurrent writers; train.Online serializes calls while
holding its own mutex.
*/
type Matrix struct {
	Vocabulary      map[string]struct{}
	VocabularyOrder []string
	Counts          map[string]map[string]float64
	Window          int
}

/*
NewMatrix allocates a co-occurrence tracker with the given window radius.
*/
func NewMatrix(window int) *Matrix {
	return &Matrix{
		Vocabulary:      make(map[string]struct{}),
		VocabularyOrder: nil,
		Counts:          make(map[string]map[string]float64),
		Window:          window,
	}
}

/*
Update records vocabulary and co-occurrence for a token slice.
*/
func (matrix *Matrix) Update(words []string) {
	for _, word := range words {
		if _, exists := matrix.Vocabulary[word]; !exists {
			matrix.Vocabulary[word] = struct{}{}
			matrix.VocabularyOrder = append(matrix.VocabularyOrder, word)
		}
	}

	for wordIndex, leftWord := range words {
		if matrix.Counts[leftWord] == nil {
			matrix.Counts[leftWord] = make(map[string]float64)
		}

		startIndex := max(0, wordIndex-matrix.Window)
		endIndex := min(len(words)-1, wordIndex+matrix.Window)

		for neighborIndex := startIndex; neighborIndex <= endIndex; neighborIndex++ {
			if neighborIndex == wordIndex {
				continue
			}

			matrix.Counts[leftWord][words[neighborIndex]]++
		}
	}
}

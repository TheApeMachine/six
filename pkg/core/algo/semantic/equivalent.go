package semantic

import (
	"math"
	"unicode/utf8"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo/levenshtein"
	"github.com/theapemachine/six/pkg/core/numeric"
)

type Equivalent struct {
	Original        string
	Mapped          string
	Similarity      float64
	vocabularyOrder []string
	coOccurrence    map[string]map[string]float64
}

func NewEquivalent(
	original string, mapped string, similarity float64,
	vocabularyOrder []string,
	coOccurrence map[string]map[string]float64,
) *Equivalent {
	return &Equivalent{
		Original:        original,
		Mapped:          mapped,
		Similarity:      similarity,
		vocabularyOrder: vocabularyOrder,
		coOccurrence:    coOccurrence,
	}
}

/*
Run performs the semantic equivalent lookup.
*/
func (equivalent *Equivalent) Run(word string) *Equivalent {
	bestWord := word
	bestSimilarity := -1.0
	bestNgramSimilarity := -1.0
	ngramBestWord := word
	editMapped := ""
	editDistBest := int(^uint(0) >> 1)

	for _, knownWord := range equivalent.vocabularyOrder {
		if math.Abs(
			float64(utf8.RuneCountInString(knownWord)-utf8.RuneCountInString(word)),
		) <= 2 {
			distance := equivalent.editDistanceUnlocked(word, knownWord)

			if distance <= core.Cfg.MarkovTrie.EditDistance {
				if distance < editDistBest {
					editDistBest = distance
					editMapped = knownWord
				}

				continue
			}
		}

		similarity := equivalent.similarityUnlocked(word, knownWord)

		if similarity > bestSimilarity {
			bestSimilarity = similarity
			bestWord = knownWord
		}

		ngramSim := numeric.CharacterNgramCosine(word, knownWord, 2)

		if ngramSim > bestNgramSimilarity {
			bestNgramSimilarity = ngramSim
			ngramBestWord = knownWord
		}
	}

	if editMapped != "" {
		return &Equivalent{
			Original:   word,
			Mapped:     editMapped,
			Similarity: core.Cfg.MarkovTrie.EditSimilarity,
		}
	}

	if bestSimilarity > 0 {
		return &Equivalent{
			Original:   word,
			Mapped:     bestWord,
			Similarity: bestSimilarity,
		}
	}

	const ngramConfidenceFloor = 0.35

	if bestNgramSimilarity >= ngramConfidenceFloor {
		return &Equivalent{
			Original:   word,
			Mapped:     ngramBestWord,
			Similarity: bestNgramSimilarity,
		}
	}

	return &Equivalent{
		Original:   word,
		Mapped:     word,
		Similarity: 0,
	}
}

func (equivalent *Equivalent) editDistanceUnlocked(left string, right string) int {
	return levenshtein.Distance(left, right)
}

/*
similarityUnlocked is co-occurrence cosine between two vocabulary keys.
*/
func (equivalent *Equivalent) similarityUnlocked(
	leftToken string, rightToken string,
) float64 {
	if leftToken == rightToken {
		return 1
	}

	leftVector := equivalent.coOccurrence[leftToken]
	rightVector := equivalent.coOccurrence[rightToken]

	if leftVector == nil || rightVector == nil {
		return 0
	}

	return numeric.CosineSparseMaps(leftVector, rightVector)
}
